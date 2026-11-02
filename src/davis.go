package main

// Davis Vantage Pro2 / Vue のコンソールからシリアル LOOP プロトコルで現在値を読み取る。
// USB / シリアルのデータロガー (6510USB / 6510SER) と、TCP 22222 番で同じプロトコルを話す WeatherLinkIP に対応。
// 参照: Davis "Vantage Pro, Vantage Pro2 and Vue Serial Communication Reference Manual" (LOOP パケット 99 バイト)

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

const (
	davisLoopLen   = 99
	davisPollEvery = 2 * time.Second
	davisInHgToHPa = 33.8639
)

// crcCCITT は Davis 方式の CRC (CCITT, 多項式 0x1021, 初期値 0)。パケット全体 (CRC 込み) に掛けると 0 になる。
func crcCCITT(b []byte) uint16 {
	var crc uint16
	for _, c := range b {
		crc ^= uint16(c) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func le16(b []byte) int { return int(b[0]) | int(b[1])<<8 }

// parseDavisLoop は LOOP パケット (99 バイト) を解析して SI 単位の気象値にする。
// 欠測 (ダッシュ値) の項目は nil。RainMidnight には 0 時からの雨量 (コンソールの日雨量) を入れる。
// 1 時間・24 時間雨量と突風はパケットに無いので呼び出し側で積算する。
func parseDavisLoop(b []byte) (*Weather, error) {
	if len(b) != davisLoopLen {
		return nil, fmt.Errorf("LOOP パケットの長さが不正です (%d バイト)", len(b))
	}
	if string(b[0:3]) != "LOO" || b[95] != '\n' || b[96] != '\r' {
		return nil, errors.New("LOOP パケットの形式が不正です")
	}
	if crcCCITT(b) != 0 {
		return nil, errors.New("LOOP パケットの CRC が一致しません")
	}
	w := &Weather{}
	// 気圧 (1/1000 inHg。コンソールが標高補正した海面気圧)
	if v := le16(b[7:]); v > 0 {
		w.Pressure = fptr(float64(v) / 1000 * davisInHgToHPa)
	}
	// 外気温 (1/10 °F, 符号付き)。32767 は欠測
	if v := int(int16(le16(b[12:]))); v != 32767 {
		w.Temp = fptr((float64(v)/10 - 32) * 5 / 9)
	}
	// 風速 (mph, 255 は欠測)
	if v := int(b[14]); v != 255 {
		w.Wind = fptr(float64(v) * mphToMs)
	}
	// 風向 (1〜360 度, 0 は欠測)
	if v := le16(b[16:]); v >= 1 && v <= 360 {
		w.WindDir = iptr(v)
	}
	// 外気湿度 (%, 255 は欠測)
	if v := int(b[33]); v >= 1 && v <= 100 {
		w.Humidity = iptr(v)
	}
	// 日射 (W/m², 32767 は欠測)
	if v := le16(b[44:]); v != 32767 {
		w.Luminosity = iptr(v)
	}
	// 日雨量 (雨量計のクリック数。1 クリック = 0.01 インチ。65535 は欠測)
	if v := le16(b[50:]); v != 65535 {
		w.RainMidnight = fptr(float64(v) * inch100Mm)
	}
	return w, nil
}

// ---------- コンソールとのやり取り ----------

var errDavisTimeout = errors.New("コンソールから応答がありません")

// davisConn は読み取りを別 goroutine で行い、タイムアウト付きで読めるようにしたもの。
type davisConn struct {
	rw   io.ReadWriteCloser
	in   chan []byte
	pend []byte

	mu  sync.Mutex
	err error
}

func newDavisConn(rw io.ReadWriteCloser) *davisConn {
	c := &davisConn{rw: rw, in: make(chan []byte, 64)}
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := rw.Read(buf)
			if n > 0 {
				c.in <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				c.mu.Lock()
				c.err = err
				c.mu.Unlock()
				close(c.in)
				return
			}
			if n == 0 {
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
	return c
}

func (c *davisConn) readErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return io.EOF
}

// flush は読み残しを捨てる。
func (c *davisConn) flush() {
	c.pend = nil
	for {
		select {
		case _, ok := <-c.in:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// read は n バイトを timeout 以内に読む。
func (c *davisConn) read(n int, timeout time.Duration) ([]byte, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(c.pend) < n {
		select {
		case b, ok := <-c.in:
			if !ok {
				return nil, c.readErr()
			}
			c.pend = append(c.pend, b...)
		case <-deadline.C:
			return nil, errDavisTimeout
		}
	}
	out := append([]byte(nil), c.pend[:n]...)
	c.pend = c.pend[n:]
	return out, nil
}

// wake はコンソールを起こす。"\n" を送ると "\n\r" が返る (最大 3 回試す)。
func (c *davisConn) wake() error {
	for i := 0; i < 3; i++ {
		c.flush()
		if _, err := c.rw.Write([]byte("\n")); err != nil {
			return err
		}
		resp, err := c.read(2, 1200*time.Millisecond)
		if err == nil && string(resp) == "\n\r" {
			return nil
		}
		if err != nil && err != errDavisTimeout {
			return err
		}
	}
	return errDavisTimeout
}

// loop は LOOP パケットを 1 つ取得する。
func (c *davisConn) loop() (*Weather, error) {
	c.flush()
	if _, err := c.rw.Write([]byte("LOOP 1\n")); err != nil {
		return nil, err
	}
	ack, err := c.read(1, time.Second)
	if err != nil {
		return nil, err
	}
	if ack[0] != 0x06 {
		return nil, errors.New("コンソールが LOOP コマンドを受け付けませんでした")
	}
	pkt, err := c.read(davisLoopLen, 3*time.Second)
	if err != nil {
		return nil, err
	}
	return parseDavisLoop(pkt)
}

// ---------- クライアント ----------

type DavisStatus struct {
	Connected bool   `json:"connected"`
	Desc      string `json:"desc"`
	Error     string `json:"error"`
	Last      int64  `json:"last"` // 最後に読めた時刻 (ミリ秒)

	Heard []string `json:"heard,omitempty"` // rtl_433 で受信できている気象センサー (新しい順)
}

type DavisClient struct {
	cfg       Config
	onReading func(*Weather, time.Time)
	onStatus  func()

	mu sync.Mutex
	st DavisStatus
}

func NewDavisClient(cfg Config, onReading func(*Weather, time.Time), onStatus func()) *DavisClient {
	d := &DavisClient{cfg: cfg, onReading: onReading, onStatus: onStatus}
	if cfg.WXSource == "davis-tcp" {
		d.st.Desc = "Davis " + davisAddr(cfg.WXTCPAddr)
	} else {
		d.st.Desc = fmt.Sprintf("Davis %s %dbps", cfg.WXSerialPort, cfg.WXBaud)
	}
	return d
}

func davisAddr(a string) string {
	if a != "" && !strings.Contains(a, ":") {
		return a + ":22222" // WeatherLinkIP の既定ポート
	}
	return a
}

func (d *DavisClient) Status() DavisStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *DavisClient) setStatus(connected bool, errText string) {
	d.mu.Lock()
	d.st.Connected, d.st.Error = connected, errText
	d.mu.Unlock()
	d.onStatus()
}

func (d *DavisClient) open(ctx context.Context) (io.ReadWriteCloser, error) {
	if d.cfg.WXSource == "davis-tcp" {
		if d.cfg.WXTCPAddr == "" {
			return nil, errors.New("WeatherLinkIP のアドレスが未設定です")
		}
		dl := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		return dl.DialContext(ctx, "tcp", davisAddr(d.cfg.WXTCPAddr))
	}
	if d.cfg.WXSerialPort == "" {
		return nil, errors.New("ロガーのシリアルポートが未設定です")
	}
	return serial.Open(d.cfg.WXSerialPort, &serial.Mode{BaudRate: d.cfg.WXBaud})
}

func (d *DavisClient) Run(ctx context.Context) {
	for {
		err := d.session(ctx)
		d.setStatus(false, errString(err))
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("気象ステーション: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (d *DavisClient) session(ctx context.Context) error {
	rw, err := d.open(ctx)
	if err != nil {
		return err
	}
	var once sync.Once
	closePort := func() { once.Do(func() { rw.Close() }) }
	defer closePort()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		closePort()
	}()

	c := newDavisConn(rw)
	if err := c.wake(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	log.Printf("気象ステーション: %s に接続しました", d.st.Desc)
	d.setStatus(true, "")

	fails := 0
	for {
		start := time.Now()
		w, err := c.loop()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			// 一時的な取りこぼしは許容し、続けて失敗したら起こし直す → それでもだめなら再接続
			if fails++; fails >= 3 {
				if err := c.wake(); err != nil {
					return err
				}
				fails = 0
			}
			d.setStatus(true, err.Error())
		} else {
			fails = 0
			now := time.Now()
			d.mu.Lock()
			d.st.Last, d.st.Error = now.UnixMilli(), ""
			d.mu.Unlock()
			d.onReading(w, now)
		}
		if wait := davisPollEvery - time.Since(start); wait > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
		}
	}
}
