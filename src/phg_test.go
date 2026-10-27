package main

import (
	"strings"
	"testing"
)

func TestPHGCode(t *testing.T) {
	cases := []struct {
		w, m, g float64
		dir     int
		want    string
	}{
		{25, 6.1, 3, 0, "PHG5130"}, // APRS101 の例: 25 W・20 ft・3 dB・無指向
		{50, 10, 6, 90, "PHG7262"}, // 49 W・40 ft・6 dB・東
		{0, 0, 0, 0, "PHG0000"},
		{200, 5000, 15, 360, "PHG9998"}, // 上限で頭打ち
	}
	for _, c := range cases {
		if got := phgCode(c.w, c.m, c.g, c.dir); got != c.want {
			t.Errorf("phgCode(%v, %v, %v, %d) = %s, want %s", c.w, c.m, c.g, c.dir, got, c.want)
		}
	}
}

func TestBeaconPHG(t *testing.T) {
	cfg := defaultConfig()
	cfg.PHGEnabled, cfg.PHGPower, cfg.PHGHeight, cfg.PHGGain = true, 25, 6.1, 3
	b := beaconBody(cfg)
	if !strings.HasSuffix(b, "E&PHG5130APRS I-Gate") {
		t.Errorf("beacon %q", b)
	}
	if p, err := ParseTNC2("JA1ABC>APKSXI:" + b); err != nil || !p.HasPos || p.Symbol() != "I&" {
		t.Errorf("parse %q -> %+v %v", b, p, err)
	}
	cfg.Comment = "PHG2360"
	if cfg.Validate() == nil {
		t.Error("duplicate PHG in comment should be rejected")
	}
}
