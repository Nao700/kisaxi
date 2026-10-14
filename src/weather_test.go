package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWeatherWithPosition(t *testing.T) {
	p, err := ParseTNC2("JA2WX>APRS,TCPIP*:=3510.00N/13655.00E_180/003g005t075r001p010P005h52b10130eMB51")
	if err != nil || p.WX == nil {
		t.Fatalf("no weather: %v %+v", err, p)
	}
	w := p.WX
	if *w.WindDir != 180 || !near(*w.Wind, 3*mphToMs) || !near(*w.Gust, 5*mphToMs) {
		t.Errorf("wind %+v", w)
	}
	if !near(*w.Temp, (75-32)*5.0/9) || !near(*w.Rain1h, 0.254) || !near(*w.Rain24h, 2.54) || !near(*w.RainMidnight, 1.27) {
		t.Errorf("temp/rain %+v", w)
	}
	if *w.Humidity != 52 || !near(*w.Pressure, 1013.0) || p.Comment != "eMB51" || p.HasCS {
		t.Errorf("hum/pres/comment %+v %q", w, p.Comment)
	}
}

func TestWeatherPositionless(t *testing.T) {
	p, _ := ParseTNC2("JA2WX>APRS:_10090556c220s004g005t-05r000p000P000h00b09900wRSW")
	w := p.WX
	if p.Type != "weather" || w == nil {
		t.Fatalf("no weather %+v", p)
	}
	if *w.WindDir != 220 || !near(*w.Wind, 4*mphToMs) || !near(*w.Temp, (-5-32)*5.0/9) || *w.Humidity != 100 || !near(*w.Pressure, 990) || p.Comment != "wRSW" {
		t.Errorf("positionless %+v %q", w, p.Comment)
	}
}

func TestWeatherMissingValues(t *testing.T) {
	p, _ := ParseTNC2("JA2WX>APRS:!3510.00N/13655.00E_.../...g...t075h..b.....Davis")
	w := p.WX
	if w == nil || w.WindDir != nil || w.Wind != nil || w.Gust != nil || w.Humidity != nil || w.Pressure != nil || !near(*w.Temp, (75-32)*5.0/9) || p.Comment != "Davis" {
		t.Errorf("missing %+v %q", w, p.Comment)
	}
	// 気象シンボルでない局はコメント中の "t075" 等を気象と誤認しない
	p, _ = ParseTNC2("JA1ABC>APRS:=3510.00N/13655.00E>test t075")
	if p.WX != nil {
		t.Errorf("false weather %+v", p.WX)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	a := NewApp(cfg, filepath.Join(dir, "aprsigate.json"), filepath.Join(dir, "logs"))
	p, _ := ParseTNC2("JA2WX>APRS:=3510.00N/13655.00E_180/003t075h52b10130")
	a.db.Update(p, "RF", time.Now(), 10)
	old, _ := ParseTNC2("JA9OLD>APRS:=3510.00N/13655.00E-old")
	a.db.Update(old, "IS", time.Now().Add(-2*time.Hour), 10)
	a.recent.Add(PacketEvent{T: time.Now().UnixMilli(), Dir: "RF", Raw: "x"})
	a.recent.Add(PacketEvent{T: time.Now().Add(-2 * time.Hour).UnixMilli(), Dir: "IS", Raw: "old"})
	a.SaveData()
	if _, err := os.Stat(a.dataPath()); err != nil {
		t.Fatal(err)
	}

	b := NewApp(cfg, a.cfgPath, a.plog.dir)
	b.LoadData()
	all := b.db.All()
	if len(all) != 1 || all[0].Call != "JA2WX" || all[0].WX == nil || *all[0].WX.Humidity != 52 || all[0].LastRF == 0 {
		t.Errorf("stations restored: %+v", all)
	}
	if pk := b.recent.All(); len(pk) != 1 || pk[0].Raw != "x" {
		t.Errorf("packets restored: %+v", pk)
	}
}
