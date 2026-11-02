package main

// KISS TNC インターフェース (シリアル / TCP)。Direwolf・UZ7HO Soundmodem 等は KISS over TCP で接続できる。

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

type TNCStatus struct {
	Connected bool   `json:"connected"`
	Desc      string `json:"desc"`
	Error     string `json:"error"`
}

type TNC struct {
	cfg      Config
	onFrame  func([]byte)
	onStatus func()

	mu sync.Mutex
	w  io.Writer
	st TNCStatus
}

func NewTNC(cfg Config, onFrame func([]byte), onStatus func()) *TNC {
	t := &TNC{cfg: cfg, onFrame: onFrame, onStatus: onStatus}
	switch cfg.TNCType {
	case "kiss-serial":
		t.st.Desc = fmt.Sprintf("KISS %s %dbps", cfg.SerialPort, cfg.Baud)
	case "kiss-tcp":
		t.st.Desc = "KISS TCP " + cfg.KissTCPAddr
	case "kiss-bt":
		name := cfg.BTName
		if name == "" {
			name = cfg.BTAddr
		}
		t.st.Desc = "KISS Bluetooth " + name
	}
	return t
}

func (t *TNC) Status() TNCStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.st
}

func (t *TNC) setStatus(connected bool, w io.Writer, errText string) {
	t.mu.Lock()
	t.st.Connected, t.w, t.st.Error = connected, w, errText
	t.mu.Unlock()
	t.onStatus()
}

// Send は TNC2 形式のパケットを AX.25 に変換して送信する。
func (t *TNC) Send(pkt string) error {
	frame, err := tnc2ToAX25(pkt)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.w == nil {
		return errors.New("TNC 未接続")
	}
	_, err = t.w.Write(kissEncode(0x00, frame))
	return err
}

func (t *TNC) open(ctx context.Context) (io.ReadWriteCloser, error) {
	switch t.cfg.TNCType {
	case "kiss-serial":
		if t.cfg.SerialPort == "" {
			return nil, errors.New("シリアルポートが未設定です")
		}
		return serial.Open(t.cfg.SerialPort, &serial.Mode{BaudRate: t.cfg.Baud})
	case "kiss-tcp":
		d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		return d.DialContext(ctx, "tcp", t.cfg.KissTCPAddr)
	case "kiss-bt":
		return openBT(ctx, t.cfg)
	}
	return nil, errors.New("TNC 種別が不正です")
}

func (t *TNC) Run(ctx context.Context) {
	for {
		err := t.session(ctx)
		t.setStatus(false, nil, errString(err))
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("TNC: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (t *TNC) session(ctx context.Context) error {
	rw, err := t.open(ctx)
	if err != nil {
		return err
	}
	// ポートは session を抜ける前に必ず閉じ終える (Once.Do は先行の Close 完了まで待つ)
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

	// KISS モード移行コマンド (TNC がコマンドモードで起動する機種向け)
	if init := strings.TrimSpace(t.cfg.TNCInit); init != "" {
		for _, line := range strings.Split(strings.ReplaceAll(init, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, err := rw.Write([]byte(line + "\r")); err != nil {
				return err
			}
			time.Sleep(500 * time.Millisecond)
		}
		time.Sleep(time.Second)
	}
	if t.cfg.TxDelayMs > 0 {
		v := t.cfg.TxDelayMs / 10
		if v > 255 {
			v = 255
		}
		rw.Write(kissEncode(0x01, []byte{byte(v)}))
	}

	log.Printf("TNC: %s に接続しました", t.st.Desc)
	t.setStatus(true, rw, "")

	var dec kissDecoder
	buf := make([]byte, 1024)
	for {
		n, err := rw.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if n == 0 {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		for _, f := range dec.Feed(buf[:n]) {
			if len(f) < 2 || f[0]&0x0F != 0 { // データフレーム以外は無視
				continue
			}
			t.onFrame(f[1:])
		}
	}
}

func listSerialPorts() []string {
	ports, err := serial.GetPortsList()
	if err != nil {
		return []string{}
	}
	return ports
}
