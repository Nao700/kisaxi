package main

import (
	"context"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// testLoop は仕様どおりの LOOP パケット (99 バイト) を組み立てる。
func testLoop(mut func(b []byte)) []byte {
	b := make([]byte, davisLoopLen)
	copy(b, "LOO")
	put16 := func(off, v int) { b[off], b[off+1] = byte(v), byte(v>>8) }
	put16(7, 29921) // 29.921 inHg
	put16(12, 685)  // 68.5 °F
	b[14] = 5       // 5 mph
	put16(16, 220)  // 220 度
	b[33] = 72      // 72 %
	put16(44, 450)  // 450 W/m²
	put16(50, 12)   // 日雨量 12 クリック = 0.12 inch
	b[95], b[96] = '\n', '\r'
	if mut != nil {
		mut(b)
	}
	crc := crcCCITT(b[:97])
	b[97], b[98] = byte(crc>>8), byte(crc)
	return b
}

func TestCRC(t *testing.T) {
	// CCITT (XMODEM) の標準テストベクタ
	if got := crcCCITT([]byte("123456789")); got != 0x31C3 {
		t.Fatalf("crc = %04x", got)
	}
	if crcCCITT(testLoop(nil)) != 0 {
		t.Fatal("CRC 込みで 0 にならない")
	}
}

func TestParseDavisLoop(t *testing.T) {
	w, err := parseDavisLoop(testLoop(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !near(*w.Pressure, 29.921*davisInHgToHPa) || math.Abs(*w.Pressure-1013.2) > 0.1 {
		t.Errorf("pressure %v", *w.Pressure)
	}
	if !near(*w.Temp, (68.5-32)*5/9) || *w.WindDir != 220 || !near(*w.Wind, 5*mphToMs) {
		t.Errorf("temp/wind %+v", w)
	}
	if *w.Humidity != 72 || *w.Luminosity != 450 || !near(*w.RainMidnight, 12*inch100Mm) {
		t.Errorf("hum/solar/rain %+v", w)
	}
	if w.Gust != nil || w.Rain1h != nil {
		t.Errorf("パケットに無い値が入っている %+v", w)
	}
}

func TestParseDavisLoopDashAndNegative(t *testing.T) {
	w, err := parseDavisLoop(testLoop(func(b []byte) {
		b[12], b[13] = 0xFF, 0x7F // 32767 = 欠測
		b[14] = 255
		b[16], b[17] = 0, 0
		b[33] = 255
		b[44], b[45] = 0xFF, 0x7F
		b[50], b[51] = 0xFF, 0xFF
		b[7], b[8] = 0, 0
	}))
	if err != nil {
		t.Fatal(err)
	}
	if w.Temp != nil || w.Wind != nil || w.WindDir != nil || w.Humidity != nil || w.Luminosity != nil || w.RainMidnight != nil || w.Pressure != nil {
		t.Errorf("欠測が残っている %+v", w)
	}
	w, _ = parseDavisLoop(testLoop(func(b []byte) { b[12], b[13] = 0xCE, 0xFF })) // -50 → -5.0 °F
	if !near(*w.Temp, (-5.0-32)*5/9) {
		t.Errorf("negative temp %v", *w.Temp)
	}
}

func TestParseDavisLoopRejects(t *testing.T) {
	good := testLoop(nil)
	bad := append([]byte(nil), good...)
	bad[20] ^= 0x01
	if _, err := parseDavisLoop(bad); err == nil || !strings.Contains(err.Error(), "CRC") {
		t.Errorf("CRC 不一致を検出できない: %v", err)
	}
	if _, err := parseDavisLoop(good[:50]); err == nil {
		t.Error("短いパケットを受け付けた")
	}
}

// 仮想コンソール: 起こす ("\n"→"\n\r")、LOOP 1 (ACK + 99 バイト) に応答する TCP サーバー。
func fakeConsole(t *testing.T, mut func(n int, b []byte)) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n := 0
		for {
			k, err := c.Read(buf)
			if err != nil {
				return
			}
			switch cmd := string(buf[:k]); cmd {
			case "\n":
				c.Write([]byte("\n\r"))
			case "LOOP 1\n":
				n++
				pkt := testLoop(func(b []byte) {
					if mut != nil {
						mut(n, b)
					}
				})
				c.Write(append([]byte{0x06}, pkt...))
			}
		}
	}()
	return ln.Addr().String(), func() { ln.Close(); wg.Wait() }
}

func TestDavisClientTCP(t *testing.T) {
	addr, stop := fakeConsole(t, func(n int, b []byte) { b[14] = byte(n) }) // 読むたびに風速が増える
	defer stop()
	cfg := defaultConfig()
	cfg.WXEnabled, cfg.WXSource, cfg.WXTCPAddr = true, "davis-tcp", addr

	got := make(chan *Weather, 4)
	d := NewDavisClient(cfg, func(w *Weather, _ time.Time) { got <- w }, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	select {
	case w := <-got:
		if *w.WindDir != 220 || !near(*w.Wind, mphToMs) {
			t.Errorf("1 回目 %+v", w)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LOOP を読めない")
	}
	if st := d.Status(); !st.Connected || st.Last == 0 {
		t.Errorf("status %+v", st)
	}
	select {
	case w := <-got:
		if !near(*w.Wind, 2*mphToMs) {
			t.Errorf("2 回目 %+v", w)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("2 回目の LOOP を読めない")
	}
}
