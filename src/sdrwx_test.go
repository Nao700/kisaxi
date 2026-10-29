package main

import (
	"testing"
	"time"
)

func TestParseRtl433(t *testing.T) {
	line := `{"time":"2026-10-29 15:00:00","model":"Fineoffset-WHx080","subtype":0,"id":123,"battery_ok":1,"temperature_C":21.5,"humidity":55,"wind_dir_deg":90,"wind_avg_km_h":7.2,"wind_max_km_h":14.4,"rain_mm":10.5}`
	d, rain, ok := parseRtl433(line)
	if !ok || d.Key != "Fineoffset-WHx080/123" {
		t.Fatalf("parse: %+v %v", d, ok)
	}
	w := d.WX
	if *w.Temp != 21.5 || *w.Humidity != 55 || *w.WindDir != 90 || *w.Wind != 2 || *w.Gust != 4 {
		t.Fatalf("weather: %+v", w)
	}
	if rain == nil || *rain != 10.5 {
		t.Fatalf("rain: %v", rain)
	}
	if _, _, ok := parseRtl433(`{"model":"Remote","id":1,"cmd":3}`); ok {
		t.Fatal("non-weather line accepted")
	}
	if _, _, ok := parseRtl433("not json"); ok {
		t.Fatal("garbage accepted")
	}
}

func TestRainTracker(t *testing.T) {
	var r rainTracker
	base := time.Date(2026, 10, 29, 12, 0, 0, 0, time.Local)
	r.add(base.Add(-2*time.Hour), 10)
	r.add(base.Add(-time.Hour), 11)
	r.add(base.Add(-30*time.Minute), 12)
	h1, h24, mid := r.add(base, 13)
	if h1 != 2 || h24 != 3 || mid != 3 {
		t.Fatalf("got %v %v %v", h1, h24, mid)
	}
	h1, _, _ = r.add(base.Add(time.Minute), 0) // 累積値のリセット
	if h1 != 0 {
		t.Fatalf("reset: %v", h1)
	}
}

func TestSdrWxCall(t *testing.T) {
	if got := sdrWxCall(Config{Callsign: "jr2tta-9"}); got != "JR2TTA-13" {
		t.Fatal(got)
	}
	if got := sdrWxCall(Config{Callsign: "JR2TTA", SdrWxCall: "jr2tta-5"}); got != "JR2TTA-5" {
		t.Fatal(got)
	}
}
