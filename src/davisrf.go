package main

// Davis ISS (屋外センサー) の無線パケットを RTL-SDR で直接受信して復調する。
// 日本仕様 (F1D 帯: 928.15〜929.65 MHz, 100 kHz 間隔 16 ch) の 2-GFSK 19.2 kbps を全チャンネル同時に受信する。
// 経路: 周波数変換 → 間引き → FIR → FM 検波 → ビット同期 → 同期語 CB89 + CRC16 検査。
// パケットは 8 バイト (ヘッダ, 風速, 風向, データ 3 バイト, CRC16)。バイトは LSB ファーストで送られる。

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

const (
	rfBitRate     = 19200.0
	rfPktLen      = 8
	rfSampleRate  = 2048000.0 // RTL-SDR のサンプルレート
	rfCenter      = 928900000.0
	rfChan1       = 928150000.0
	rfChanStep    = 100000.0
	rfChanCount   = 16
	rfSensorTTL   = 15 * time.Minute // 気温・湿度などはこの時間届かなければ欠測にする
	rfDupWindow   = 150 * time.Millisecond
	rfBlockSample = 1 << 15
)

// rfChannel は 1 チャンネル分の受信機: 周波数変換 → 平均間引き → FIR 間引き → FM 検波 → ビット同期 → 同期語・CRC 検索。
type rfChannel struct {
	Index int
	Freq  float64

	c, s   float64
	rc, rs float64

	d1   int
	cnt1 int
	ar   float64
	ai   float64

	taps  []float64
	hr    []float64
	hi    []float64
	hpos  int
	d2    int
	cnt2  int
	fs2   float64
	sps   float64
	prevR float64
	prevI float64

	pEMA  float64
	noise float64

	mean    float64
	hiEnv   float64
	loEnv   float64
	phase   float64
	sampled bool
	prevBit bool
	hiW     uint64
	loW     uint64
	nbits   uint64

	onPacket func(ch *rfChannel, raw [rfPktLen]byte)
}

func newRFChannel(idx int, freq, center, sr float64) *rfChannel {
	ch := &rfChannel{Index: idx, Freq: freq}
	w := -2 * math.Pi * (freq - center) / sr
	ch.c, ch.s = 1, 0
	ch.rc, ch.rs = math.Cos(w), math.Sin(w)
	ch.d1 = int(math.Round(sr / 300000))
	if ch.d1 < 1 {
		ch.d1 = 1
	}
	fs1 := sr / float64(ch.d1)
	ch.d2 = 4
	ch.fs2 = fs1 / float64(ch.d2)
	ch.sps = ch.fs2 / rfBitRate
	n := 21
	fc := 30000.0 / fs1
	ch.taps = make([]float64, n)
	sum := 0.0
	for i := 0; i < n; i++ {
		x := float64(i) - float64(n-1)/2
		var h float64
		if x == 0 {
			h = 2 * fc
		} else {
			h = math.Sin(2*math.Pi*fc*x) / (math.Pi * x)
		}
		win := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		ch.taps[i] = h * win
		sum += ch.taps[i]
	}
	for i := range ch.taps {
		ch.taps[i] /= sum
	}
	ch.hr = make([]float64, n)
	ch.hi = make([]float64, n)
	ch.noise = -1
	return ch
}

// process は正規化済み (±1) の IQ ブロックを処理する。
func (ch *rfChannel) process(I, Q []float64) {
	d1 := ch.d1
	inv := 1.0 / float64(d1)
	c, s, rc, rs := ch.c, ch.s, ch.rc, ch.rs
	for k := 0; k < len(I); k++ {
		xr := I[k]*c - Q[k]*s
		xi := I[k]*s + Q[k]*c
		c, s = c*rc-s*rs, c*rs+s*rc
		ch.ar += xr
		ch.ai += xi
		ch.cnt1++
		if ch.cnt1 == d1 {
			ch.stage2(ch.ar*inv, ch.ai*inv)
			ch.ar, ch.ai, ch.cnt1 = 0, 0, 0
		}
	}
	m := math.Hypot(c, s)
	ch.c, ch.s = c/m, s/m
}

func (ch *rfChannel) stage2(r, i float64) {
	n := len(ch.taps)
	ch.hr[ch.hpos] = r
	ch.hi[ch.hpos] = i
	ch.hpos++
	if ch.hpos == n {
		ch.hpos = 0
	}
	ch.cnt2++
	if ch.cnt2 < ch.d2 {
		return
	}
	ch.cnt2 = 0
	var yr, yi float64
	p := ch.hpos
	for k := 0; k < n; k++ {
		yr += ch.taps[k] * ch.hr[p]
		yi += ch.taps[k] * ch.hi[p]
		p++
		if p == n {
			p = 0
		}
	}
	ch.demod(yr, yi)
}

func (ch *rfChannel) demod(r, i float64) {
	pw := r*r + i*i
	ch.pEMA += (pw - ch.pEMA) * 0.02
	if ch.noise < 0 {
		ch.noise = pw
	}
	if ch.pEMA < ch.noise*4 {
		ch.noise += (ch.pEMA - ch.noise) * 0.0005
	}
	y := math.Atan2(i*ch.prevR-r*ch.prevI, r*ch.prevR+i*ch.prevI)
	ch.prevR, ch.prevI = r, i

	yc := y
	if yc > 1.5 {
		yc = 1.5
	} else if yc < -1.5 {
		yc = -1.5
	}
	// 同じビットが続いても崩れないよう、上下の包絡線の中央をスライスレベルにする
	if yc > ch.hiEnv {
		ch.hiEnv = yc
	} else {
		ch.hiEnv += (ch.mean - ch.hiEnv) * 0.004
	}
	if yc < ch.loEnv {
		ch.loEnv = yc
	} else {
		ch.loEnv += (ch.mean - ch.loEnv) * 0.004
	}
	ch.mean = (ch.hiEnv + ch.loEnv) / 2
	cur := y > ch.mean
	if cur != ch.prevBit {
		e := ch.phase
		if e > ch.sps/2 {
			e -= ch.sps
		}
		ch.phase -= 0.3 * e
	}
	ch.prevBit = cur
	ch.phase++
	if ch.phase >= ch.sps {
		ch.phase -= ch.sps
		ch.sampled = false
	}
	if !ch.sampled && ch.phase >= ch.sps/2 {
		ch.sampled = true
		ch.pushBit(cur)
	}
}

func rfRev8(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r = r<<1 | (b>>uint(i))&1
	}
	return r
}

// rfSyncPats は同期語 CB89 と、極性反転・ビット順反転の組み合わせ。
var rfSyncPats = []struct {
	pat uint16
	inv bool
}{
	{0xCB89, false},
	{0x3476, true},
}

func (ch *rfChannel) pushBit(b bool) {
	ch.hiW = ch.hiW<<1 | ch.loW>>63
	ch.loW <<= 1
	if b {
		ch.loW |= 1
	}
	ch.nbits++
	if ch.nbits < 80 { // 同期語 16 + パケット 64
		return
	}
	if ch.noise <= 0 || ch.pEMA < ch.noise*4 {
		return // 搬送波が無いときの偶然の一致は無視する
	}
	syn := uint16(ch.hiW & 0xFFFF)
	for _, sp := range rfSyncPats {
		if syn != sp.pat {
			continue
		}
		lo := ch.loW
		if sp.inv {
			lo = ^lo
		}
		var raw [rfPktLen]byte
		for k := 0; k < rfPktLen; k++ {
			raw[k] = rfRev8(byte(lo >> uint(56-8*k))) // Davis はバイトを LSB から送る
		}
		if crcCCITT(raw[:]) == 0 && ch.onPacket != nil {
			ch.onPacket(ch, raw)
		}
		return
	}
}

// ---------- パケットの解釈 ----------

type rfPacket struct {
	ID      int // 1..8
	Type    int
	BattLow bool
	WindMph int
	WindDir int // 0 = 欠測
	Raw     [rfPktLen]byte
}

func parseRFPacket(raw [rfPktLen]byte) rfPacket {
	b := raw[:]
	p := rfPacket{Raw: raw}
	p.Type = int(b[0] >> 4)
	p.ID = int(b[0]&7) + 1
	p.BattLow = b[0]&8 != 0
	p.WindMph = int(b[1])
	if b[2] != 0 {
		p.WindDir = int(9 + float64(b[2])*342.0/255.0)
	}
	return p
}

// rfSensors は受信したセンサー値の最新を保持し、Weather にまとめる。
type rfSensors struct {
	mu     sync.Mutex
	rainMm float64 // 雨量計 1 回転 (クリック) あたりの mm

	temp, hum, sol struct {
		v float64
		t time.Time
	}
	wind struct {
		mph, dir int
		t        time.Time
	}
	tips    int // 直近の転倒カウンタ (0〜127)
	tipsSet bool
	dayMm   float64
	dayKey  string
}

// ingest はパケットを取り込み、現在値をまとめた Weather を返す。
func (s *rfSensors) ingest(p rfPacket, now time.Time) *Weather {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := p.Raw[:]
	s.wind.mph, s.wind.t = p.WindMph, now
	if p.WindDir > 0 || p.WindMph == 0 {
		s.wind.dir = p.WindDir
	}
	switch p.Type {
	case 0x8: // 外気温 (1/160 °F)
		raw := int16(uint16(b[3])<<8 | uint16(b[4]))
		tf := float64(raw) / 160.0
		if tf > -60 && tf < 160 {
			s.temp.v, s.temp.t = (tf-32)*5/9, now
		}
	case 0xA: // 湿度 (1/10 %)
		h := float64(uint16(b[4]>>4)<<8|uint16(b[3])) / 10.0
		if h >= 1 && h <= 100 {
			s.hum.v, s.hum.t = h, now
		}
	case 0x6: // 日射
		if b[3] != 0xFF {
			s.sol.v, s.sol.t = float64((int(b[3])<<2|int(b[4]>>6)))*1.757936, now
		}
	case 0xE: // 雨量計の転倒カウンタ (7 ビットで 1 周する)
		tips := int(b[3] & 0x7F)
		if s.tipsSet {
			s.dayMm += float64((tips-s.tips+128)%128) * s.rainMm
		}
		s.tips, s.tipsSet = tips, true
	}
	key := now.Format("2006-01-02")
	if s.dayKey != key {
		s.dayKey, s.dayMm = key, 0
		// 日付が変わった直後にカウンタが進んでいた場合はその分を失うが、5 分以内の誤差
	}
	w := &Weather{}
	w.Wind = fptr(float64(s.wind.mph) * mphToMs)
	if s.wind.dir > 0 {
		w.WindDir = iptr(s.wind.dir)
	}
	if now.Sub(s.temp.t) <= rfSensorTTL && !s.temp.t.IsZero() {
		w.Temp = fptr(s.temp.v)
	}
	if now.Sub(s.hum.t) <= rfSensorTTL && !s.hum.t.IsZero() {
		w.Humidity = iptr(int(math.Round(s.hum.v)))
	}
	if now.Sub(s.sol.t) <= rfSensorTTL && !s.sol.t.IsZero() {
		w.Luminosity = iptr(int(math.Round(s.sol.v)))
	}
	w.RainMidnight = fptr(s.dayMm)
	return w
}

// ---------- IQ 入力 ----------

// iqStream は RTL-SDR などの IQ 入力 (8 ビット unsigned の I,Q 交互)。
type iqStream interface {
	Read(p []byte) (int, error)
	Close() error
}

// openIQ は設定から IQ 入力を開く。プラットフォーム固有の実装 (rtlsdr_*.go) が openRTLSDR を提供する。テストでは差し替える。
var openIQ = func(cfg Config) (iqStream, string, error) { return openRTLSDR(cfg) }

// ---------- クライアント ----------

type DavisRFClient struct {
	cfg       Config
	onReading func(*Weather, time.Time)
	onStatus  func()

	sens rfSensors

	mu       sync.Mutex
	st       DavisStatus
	lastRaw  [rfPktLen]byte
	lastTime time.Time
}

func NewDavisRFClient(cfg Config, onReading func(*Weather, time.Time), onStatus func()) *DavisRFClient {
	d := &DavisRFClient{cfg: cfg, onReading: onReading, onStatus: onStatus}
	d.sens.rainMm = cfg.WXRainMm
	if d.sens.rainMm <= 0 {
		d.sens.rainMm = 0.2
	}
	d.st.Desc = fmt.Sprintf("Davis SDR (ID%d)", cfg.WXSDRID)
	return d
}

func (d *DavisRFClient) Status() DavisStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *DavisRFClient) setStatus(connected bool, errText string) {
	d.mu.Lock()
	d.st.Connected, d.st.Error = connected, errText
	d.mu.Unlock()
	d.onStatus()
}

func (d *DavisRFClient) Run(ctx context.Context) {
	for {
		err := d.session(ctx)
		d.setStatus(false, errString(err))
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("気象ステーション (SDR): %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (d *DavisRFClient) handle(ch *rfChannel, raw [rfPktLen]byte) {
	now := time.Now()
	p := parseRFPacket(raw)
	if p.ID != d.cfg.WXSDRID {
		return
	}
	d.mu.Lock()
	dup := raw == d.lastRaw && now.Sub(d.lastTime) < rfDupWindow
	d.lastRaw, d.lastTime = raw, now
	if !dup {
		d.st.Last, d.st.Error = now.UnixMilli(), ""
		d.st.Detail = fmt.Sprintf("ch%02d %.2fMHz", ch.Index, ch.Freq/1e6)
		d.st.BattLow = p.BattLow
	}
	d.mu.Unlock()
	if dup {
		return
	}
	d.onReading(d.sens.ingest(p, now), now)
}

func (d *DavisRFClient) session(ctx context.Context) error {
	src, desc, err := openIQ(d.cfg)
	if err != nil {
		return err
	}
	var once sync.Once
	closeSrc := func() { once.Do(func() { src.Close() }) }
	defer closeSrc()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		closeSrc()
	}()
	log.Printf("気象ステーション (SDR): %s で受信を開始しました", desc)
	d.mu.Lock()
	d.st.Desc = fmt.Sprintf("Davis SDR (ID%d) %s", d.cfg.WXSDRID, desc)
	d.mu.Unlock()
	d.setStatus(true, "")

	chans := make([]*rfChannel, rfChanCount)
	var cmu sync.Mutex
	for i := range chans {
		chans[i] = newRFChannel(i+1, rfChan1+float64(i)*rfChanStep, rfCenter, rfSampleRate)
		chans[i].onPacket = func(ch *rfChannel, raw [rfPktLen]byte) {
			cmu.Lock()
			defer cmu.Unlock()
			d.handle(ch, raw)
		}
	}
	nw := len(chans)
	buf := make([]byte, 2*rfBlockSample)
	I := make([]float64, rfBlockSample)
	Q := make([]float64, rfBlockSample)
	var wg sync.WaitGroup
	stale := time.Now()
	for {
		n, err := readFull(src, buf)
		if ctx.Err() != nil {
			return nil
		}
		if n >= 2 {
			cnt := n / 2
			for k := 0; k < cnt; k++ {
				I[k] = (float64(buf[2*k]) - 127.5) / 127.5
				Q[k] = (float64(buf[2*k+1]) - 127.5) / 127.5
			}
			wg.Add(nw)
			for _, c := range chans {
				go func(c *rfChannel) { defer wg.Done(); c.process(I[:cnt], Q[:cnt]) }(c)
			}
			wg.Wait()
			stale = time.Now()
		}
		if err != nil {
			return err
		}
		if time.Since(stale) > 10*time.Second {
			return errors.New("SDR からデータが届きません")
		}
	}
}

func readFull(r iqStream, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
