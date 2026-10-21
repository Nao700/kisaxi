package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDXStatusText(t *testing.T) {
	ts := time.Date(2026, 10, 21, 8, 20, 0, 0, time.Local).UnixMilli()
	r := DXRecord{Call: "JJ2YKR-2", Lat: 35 + 29.80/60, Lon: 137 + 15.28/60, Km: 46.83, Bearing: 80, T: ts}
	if got, want := dxStatusText(r, "mi"), "DX: JJ2YKR-2 35.29.80N 137.15.28E 29.1 miles 80deg 08:20"; got != want {
		t.Errorf("mi: %q want %q", got, want)
	}
	if got, want := dxStatusText(r, "km"), "DX: JJ2YKR-2 35.29.80N 137.15.28E 46.8 km 80deg 08:20"; got != want {
		t.Errorf("km: %q want %q", got, want)
	}
}

func TestDXCandidate(t *testing.T) {
	cases := map[string]bool{
		"JA1ABC>APRS:=3510.00N/13655.00E>":                 true,
		"JA1ABC>APRS,WIDE1-1,WIDE2-1:=3510.00N/13655.00E>": true,  // 未使用パスは直接受信
		"JA1ABC>APRS,JA2DIG*,WIDE2-1:=3510.00N/13655.00E>": false, // デジ経由
		"JA1ABC>APRS:=3510.  N/13655.  E>":                 false, // 位置あいまい
		"JA1ABC>APRS:;OBJ      *111111z3510.00N/13655.00E>": false, // オブジェクト
		"JA1ABC>APRS:>status only":                         false,
	}
	for line, want := range cases {
		p, err := ParseTNC2(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if got := dxCandidate(p); got != want {
			t.Errorf("%s: got %v want %v", line, got, want)
		}
	}
}

func TestDXRecordAndReset(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.Lat, cfg.Lon = 35.0, 137.0
	cfg.DXReset = "hour"
	a := NewApp(cfg, filepath.Join(dir, "aprsigate.json"), filepath.Join(dir, "logs"))
	now := time.Date(2026, 10, 21, 8, 10, 0, 0, time.Local)
	near, _ := ParseTNC2("JA1NEAR>APRS:=3510.00N/13700.00E>")
	far, _ := ParseTNC2("JA1FAR>APRS:=3530.00N/13700.00E>")
	a.checkDX(near, cfg, now)
	a.checkDX(far, cfg, now.Add(time.Minute))
	a.checkDX(near, cfg, now.Add(2*time.Minute))
	if r := a.DX(); r == nil || r.Call != "JA1FAR" || r.Bearing != 0 || r.Km < 55 || r.Km > 56 || !a.dxPending {
		t.Fatalf("record: %+v pending=%v", r, a.dxPending)
	}
	a.tickDX(now.Add(time.Hour))
	if a.DX() != nil || a.dxPending {
		t.Errorf("not reset at hour change: %+v", a.DX())
	}
}

func TestDXPersist(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.Lat, cfg.Lon = 35.0, 137.0
	a := NewApp(cfg, filepath.Join(dir, "aprsigate.json"), filepath.Join(dir, "logs"))
	far, _ := ParseTNC2("JA1FAR>APRS:=3530.00N/13700.00E>")
	a.checkDX(far, cfg, time.Now())
	a.SaveData()
	b := NewApp(cfg, a.cfgPath, a.plog.dir)
	b.LoadData()
	if r := b.DX(); r == nil || r.Call != "JA1FAR" || b.dxPending {
		t.Errorf("restored: %+v pending=%v", r, b.dxPending)
	}
}
