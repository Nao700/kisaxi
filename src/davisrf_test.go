package main

import (
	"bytes"
	"context"
	"io"
	"math"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
)

func rfBuildPacket(id, typ int, b1, b2, b3, b4 byte, batt bool) [rfPktLen]byte {
	var p [rfPktLen]byte
	p[0] = byte(typ<<4) | byte((id-1)&7)
	if batt {
		p[0] |= 8
	}
	p[1], p[2], p[3], p[4] = b1, b2, b3, b4
	c := crcCCITT(p[:6])
	p[6], p[7] = byte(c>>8), byte(c)
	return p
}

// rfBits は実際の電波と同じ並び: プリアンブル AAAA, 同期語 CB89 (MSB 先頭), 本体 8 バイト (LSB 先頭)。
func rfBits(p [rfPktLen]byte) []bool {
	var bits []bool
	put := func(b byte, lsb bool) {
		for i := 0; i < 8; i++ {
			if lsb {
				bits = append(bits, (b>>uint(i))&1 == 1)
			} else {
				bits = append(bits, (b>>uint(7-i))&1 == 1)
			}
		}
	}
	for _, b := range []byte{0xAA, 0xAA, 0xCB, 0x89} {
		put(b, false)
	}
	for _, b := range p {
		put(b, true)
	}
	return bits
}

type rfTx struct {
	at   float64
	freq float64
	pkt  [rfPktLen]byte
}

// rfSynth は 2-FSK (偏移 ±9.6 kHz) の信号と雑音を cu8 の IQ にする。
func rfSynth(dur float64, txs []rfTx) []byte {
	sr := rfSampleRate
	n := int(dur * sr)
	I := make([]float64, n)
	Q := make([]float64, n)
	rng := rand.New(rand.NewSource(1))
	for i := range I {
		I[i] = rng.NormFloat64() * 0.02
		Q[i] = rng.NormFloat64() * 0.02
	}
	for _, tx := range txs {
		bits := rfBits(tx.pkt)
		sps := sr / rfBitRate
		start := int(tx.at * sr)
		phase := 0.0
		total := int(float64(len(bits)) * sps)
		for k := 0; k < total && start+k < n; k++ {
			dev := -9600.0
			if bits[int(float64(k)/sps)] {
				dev = 9600.0
			}
			phase += 2 * math.Pi * (tx.freq - rfCenter + dev) / sr
			I[start+k] += 0.25 * math.Cos(phase)
			Q[start+k] += 0.25 * math.Sin(phase)
		}
	}
	out := make([]byte, 2*n)
	clip := func(v float64) byte { return byte(math.Max(0, math.Min(255, v*127.5+127.5))) }
	for i := 0; i < n; i++ {
		out[2*i], out[2*i+1] = clip(I[i]), clip(Q[i])
	}
	return out
}

type nopCloseReader struct{ io.Reader }

func (nopCloseReader) Close() error { return nil }

func runRF(t *testing.T, cfg Config, data []byte) []*Weather {
	t.Helper()
	old := openIQ
	openIQ = func(Config) (iqStream, string, error) { return nopCloseReader{bytes.NewReader(data)}, "test", nil }
	defer func() { openIQ = old }()
	var mu sync.Mutex
	var got []*Weather
	d := NewDavisRFClient(cfg, func(w *Weather, _ time.Time) { mu.Lock(); got = append(got, w); mu.Unlock() }, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := d.session(ctx); err == nil {
		t.Fatal("終端で EOF が返るはず")
	}
	mu.Lock()
	defer mu.Unlock()
	return got
}

func TestRFDecodeHoppingChannels(t *testing.T) {
	cfg := defaultConfig()
	var txs []rfTx
	at := 0.2
	for k, ch := range []int{3, 9, 14, 0, 7} {
		f := rfChan1 + float64(ch)*rfChanStep + 1500
		// 気温 (type 8) と湿度 (type A)。他局 (ID2) のパケットは無視される
		raw := int16(math.Round((20.0*9/5 + 32) * 160)) // 20 ℃
		txs = append(txs,
			rfTx{at, f, rfBuildPacket(1, 8, byte(5+k), byte(30+k*20), byte(uint16(raw)>>8), byte(raw), false)},
			rfTx{at + 0.1, f, rfBuildPacket(1, 0xA, byte(5+k), byte(30+k*20), 0xC8, 0x20, false)}, // 0x2C8 = 712 → 71.2%
			rfTx{at + 0.2, f, rfBuildPacket(2, 8, 99, 99, 0, 0, false)},
		)
		at += 0.45
	}
	got := runRF(t, cfg, rfSynth(at+0.3, txs))
	if len(got) != 10 {
		t.Fatalf("取得数 %d (期待 10)", len(got))
	}
	last := got[len(got)-1]
	if last.Temp == nil || math.Abs(*last.Temp-20) > 0.1 {
		t.Fatalf("気温 %v", last.Temp)
	}
	if last.Humidity == nil || *last.Humidity != 71 {
		t.Fatalf("湿度 %v", last.Humidity != nil)
	}
	if last.Wind == nil || math.Abs(*last.Wind-9*mphToMs) > 0.01 {
		t.Fatalf("風速 %v", last.Wind)
	}
	if last.WindDir == nil || *last.WindDir < 100 {
		t.Fatalf("風向 %v", last.WindDir)
	}
	if got[0].Humidity != nil {
		t.Fatalf("最初のパケット時点では湿度は欠測のはず")
	}
}

func TestRFNoPacketInNoise(t *testing.T) {
	got := runRF(t, defaultConfig(), rfSynth(1.0, nil))
	if len(got) != 0 {
		t.Fatalf("雑音から %d 個のパケットを誤検出", len(got))
	}
}

func TestRFRainTips(t *testing.T) {
	var s rfSensors
	s.rainMm = 0.2
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)
	tip := func(n int) *Weather {
		now = now.Add(10 * time.Second)
		return s.ingest(parseRFPacket(rfBuildPacket(1, 0xE, 0, 0, byte(n), 0, false)), now)
	}
	tip(126)
	w := tip(2) // 126 → 127 → 0 → 1 → 2 : 4 回転倒 (7 ビットカウンタの桁あふれ)
	if w.RainMidnight == nil || math.Abs(*w.RainMidnight-0.8) > 1e-9 {
		t.Fatalf("日雨量 %v", w.RainMidnight)
	}
	now = now.Add(24 * time.Hour)
	w = tip(2)
	if *w.RainMidnight != 0 {
		t.Fatalf("日付が変わったら 0 に戻る: %v", *w.RainMidnight)
	}
}

func TestRFStaleTemperature(t *testing.T) {
	var s rfSensors
	s.rainMm = 0.2
	now := time.Now()
	raw := int16(70 * 160)
	s.ingest(parseRFPacket(rfBuildPacket(1, 8, 0, 0, byte(uint16(raw)>>8), byte(raw), false)), now)
	w := s.ingest(parseRFPacket(rfBuildPacket(1, 9, 3, 0, 0, 0, false)), now.Add(rfSensorTTL+time.Minute))
	if w.Temp != nil {
		t.Fatal("古い気温は欠測にする")
	}
}

// 実際の librtlsdr 経由の経路を、偽のライブラリ (testdata/fake_rtlsdr.c) で確かめる。
// 使い方: gcc -shared -fPIC -o /tmp/fake/librtlsdr.so.0 testdata/fake_rtlsdr.c && LD_LIBRARY_PATH=/tmp/fake go test -run LibRTL
// ライブラリが見つからないときは何もしない。
func TestRTLSDRLibrary(t *testing.T) {
	if _, err := loadRTL(); err != nil {
		t.Skip("librtlsdr がありません: ", err)
	}
	raw := int16(math.Round((18.0*9/5 + 32) * 160))
	f := rfChan1 + 5*rfChanStep + 1000
	data := rfSynth(0.6, []rfTx{{0.2, f, rfBuildPacket(1, 8, 4, 100, byte(uint16(raw)>>8), byte(raw), false)}})
	path := t.TempDir() + "/iq.cu8"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_IQ", path)
	var mu sync.Mutex
	var got []*Weather
	d := NewDavisRFClient(defaultConfig(), func(w *Weather, _ time.Time) { mu.Lock(); got = append(got, w); mu.Unlock() }, func() {})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.session(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Temp == nil || math.Abs(*got[0].Temp-18) > 0.1 {
		t.Fatalf("取得 %d 件", len(got))
	}
}
