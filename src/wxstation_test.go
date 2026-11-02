package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newWXTestApp(t *testing.T) *App {
	t.Helper()
	cfg := defaultConfig()
	cfg.Callsign, cfg.WXEnabled = "JR2TTA", true
	cfg.Lat, cfg.Lon = 35.6812, 139.7671
	return NewApp(cfg, filepath.Join(t.TempDir(), "aprsigate.json"), t.TempDir())
}

func TestWXDataFormat(t *testing.T) {
	w := &Weather{WindDir: iptr(220), Wind: fptr(4 * mphToMs), Gust: fptr(9 * mphToMs), Temp: fptr((64.0 - 32) * 5 / 9),
		Rain1h: fptr(0), Rain24h: fptr(12 * inch100Mm), RainMidnight: fptr(4 * inch100Mm),
		Humidity: iptr(72), Pressure: fptr(1013.2), Luminosity: iptr(450)}
	if got, want := wxData(w), "220/004g009t064r000p012P004h72b10132L450"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// 湿度 100% は h00、氷点下は t-05、1000 W/m² 以上は l
	w = &Weather{WindDir: iptr(360), Wind: fptr(0), Gust: fptr(0), Temp: fptr((-5.0 - 32) * 5 / 9), Humidity: iptr(100), Luminosity: iptr(1234)}
	if got, want := wxData(w), "360/000g000t-05h00l234"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// 欠測
	if got, want := wxData(&Weather{}), ".../...g...t..."; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// 送信用に組み立てたパケットを、受信側の既存の解析でそのまま読み戻せること
func TestWXPacketRoundTrip(t *testing.T) {
	w := &Weather{WindDir: iptr(220), Wind: fptr(4 * mphToMs), Gust: fptr(9 * mphToMs), Temp: fptr((64.0 - 32) * 5 / 9),
		Rain1h: fptr(0.254), Rain24h: fptr(12 * inch100Mm), RainMidnight: fptr(4 * inch100Mm),
		Humidity: iptr(72), Pressure: fptr(1013.2), Luminosity: iptr(450)}
	now := time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC)
	body := wxPacketBody(35.6812, 139.7671, w, now)
	if body[:8] != "@151230z" {
		t.Fatalf("time %q", body)
	}
	p, err := ParseTNC2("JR2TTA-13>APKSXI,TCPIP*:" + body)
	if err != nil || p.WX == nil || !p.HasPos {
		t.Fatalf("parse: %v %+v", err, p)
	}
	r := p.WX
	if *r.WindDir != 220 || !near(*r.Wind, 4*mphToMs) || !near(*r.Gust, 9*mphToMs) || *r.Humidity != 72 || *r.Luminosity != 450 {
		t.Errorf("roundtrip %+v", r)
	}
	if !near(*r.Pressure, 1013.2) || !near(*r.Rain1h, 0.254) || !near(*r.Rain24h, 3.048) || !near(*r.Temp, 17.7778) {
		t.Errorf("roundtrip %+v", r)
	}
	if p.Symbol() != "/_" || !near(p.Lat, 35.6812) {
		t.Errorf("symbol %q lat %v", p.Symbol(), p.Lat)
	}
}

func TestWXCallsign(t *testing.T) {
	c := defaultConfig()
	c.Callsign = "JR2TTA"
	if got := wxCallsign(c); got != "JR2TTA-13" {
		t.Errorf("%q", got)
	}
	c.Callsign = "JR2TTA-10"
	if got := wxCallsign(c); got != "JR2TTA-13" {
		t.Errorf("%q", got)
	}
	c.Callsign = "JR2TTA-13"
	if got := wxCallsign(c); got != "JR2TTA-14" {
		t.Errorf("%q", got)
	}
	c.WXCall = "JR2TTA-5"
	if got := wxCallsign(c); got != "JR2TTA-5" {
		t.Errorf("%q", got)
	}
	c.WXCall = "JR2TTA-13"
	c.Callsign = "JR2TTA-13"
	c.Normalize()
	if err := c.Validate(); err == nil {
		t.Error("自局と同じコールを受け付けた")
	}
}

func TestWXOwnCallIgnored(t *testing.T) {
	c := defaultConfig()
	c.Callsign, c.WXEnabled = "JR2TTA", true
	if !isOwnCall("JR2TTA", c) || !isOwnCall("JR2TTA-13", c) || isOwnCall("JR2TTA-7", c) {
		t.Error("isOwnCall")
	}
	c.WXEnabled = false
	if isOwnCall("JR2TTA-13", c) {
		t.Error("無効のときは気象局を自局扱いしない")
	}
}

func TestWXRainAccumulation(t *testing.T) {
	a := newWXTestApp(t)
	t0 := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	read := func(at time.Time, dayMm float64) {
		a.wxIngest(&Weather{Temp: fptr(20), Wind: fptr(2), RainMidnight: fptr(dayMm)}, at)
	}
	read(t0, 5)                         // 起動直後: 基準だけ作り、増分は数えない
	read(t0.Add(10*time.Minute), 5.254) // 1 クリック
	read(t0.Add(50*time.Minute), 6.016) // 3 クリック
	a.wx.mu.Lock()
	cur := a.wx.cur
	a.wx.mu.Unlock()
	if !near(*cur.Rain1h, 4*0.254) || !near(*cur.Rain24h, 4*0.254) || !near(*cur.RainMidnight, 6.016) {
		t.Errorf("rain %v %v %v", *cur.Rain1h, *cur.Rain24h, *cur.RainMidnight)
	}
	// 1 時間以上たつと 1 時間雨量から外れ、24 時間雨量には残る
	read(t0.Add(75*time.Minute), 6.016)
	a.wx.mu.Lock()
	cur = a.wx.cur
	a.wx.mu.Unlock()
	if !near(*cur.Rain1h, 3*0.254) || !near(*cur.Rain24h, 4*0.254) {
		t.Errorf("1h/24h %v %v", *cur.Rain1h, *cur.Rain24h)
	}
	// 0 時で日雨量が戻っても、新しい日雨量を増分として数える
	read(t0.Add(80*time.Minute), 0.254)
	a.wx.mu.Lock()
	cur = a.wx.cur
	a.wx.mu.Unlock()
	if !near(*cur.Rain24h, 5*0.254) {
		t.Errorf("midnight reset %v", *cur.Rain24h)
	}
}

func TestWXGustAndHistory(t *testing.T) {
	a := newWXTestApp(t)
	t0 := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	for i, v := range []float64{2, 7, 3} {
		a.wxIngest(&Weather{Temp: fptr(20 + float64(i)), Wind: fptr(v), Humidity: iptr(50)}, t0.Add(time.Duration(i)*2*time.Second))
	}
	a.wx.mu.Lock()
	g := *a.wx.cur.Gust
	a.wx.mu.Unlock()
	if g != 7 {
		t.Errorf("gust %v", g)
	}
	// 5 分以上たつと突風は新しい最大値になり、前の区間は履歴に確定する
	a.wxIngest(&Weather{Temp: fptr(30), Wind: fptr(1)}, t0.Add(6*time.Minute))
	s := a.WX()
	if len(s.History) != 2 {
		t.Fatalf("history %d", len(s.History))
	}
	h := s.History[0]
	if !near(*h.Temp, 21) || !near(*h.TempMin, 20) || !near(*h.TempMax, 22) || *h.Gust != 7 || !near(*h.Wind, 4) || !near(*h.Hum, 50) {
		t.Errorf("bucket %+v", h)
	}
	if *s.Cur.Gust != 1 {
		t.Errorf("gust after window %v", *s.Cur.Gust)
	}
}

func TestWXSaveLoad(t *testing.T) {
	a := newWXTestApp(t)
	now := time.Now()
	a.wxIngest(&Weather{Temp: fptr(20), RainMidnight: fptr(1)}, now.Add(-2*time.Minute))
	a.wxIngest(&Weather{Temp: fptr(21), RainMidnight: fptr(1.254)}, now.Add(-time.Minute))
	a.SaveWX()

	b := NewApp(a.Config(), a.cfgPath, t.TempDir())
	b.LoadWX()
	s := b.WX()
	if len(s.History) == 0 {
		t.Fatal("履歴が復元されない")
	}
	b.wx.mu.Lock()
	n, base := len(b.wx.rain), b.wx.baseMm
	b.wx.mu.Unlock()
	if n != 1 || !near(base, 1.254) {
		t.Errorf("rain events %d base %v", n, base)
	}
	// 再起動直後でも、続きの雨量が増分として数えられる
	b.wxIngest(&Weather{Temp: fptr(21), RainMidnight: fptr(1.508)}, now)
	b.wx.mu.Lock()
	r := *b.wx.cur.Rain24h
	b.wx.mu.Unlock()
	if !near(r, 2*0.254) {
		t.Errorf("rain24h after restart %v", r)
	}
}
