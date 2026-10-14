package main

import (
	"strings"
	"testing"
	"time"
)

func objCfg(objs ...ObjectDef) Config {
	c := defaultConfig()
	c.Callsign = "JR2TTA-10"
	c.BeaconRFPath = "WIDE1-1"
	c.Objects = objs
	return c
}

func TestObjectPackets(t *testing.T) {
	now := time.Date(2026, 10, 11, 2, 33, 0, 0, time.UTC)
	cfg := objCfg()
	o := ObjectDef{Kind: "object", Name: "HAMFES", Lat: 35.17083, Lon: 136.88167, Sym: "/;", Comment: "HAM FES", Via: "BOTH", IntervalMin: 30, Enabled: true}
	rf, is := objectLines(cfg, o, false, now)
	if rf != "JR2TTA-10>APZJIG,WIDE1-1:;HAMFES   *110233z3510.25N/13652.90E;HAM FES" {
		t.Errorf("object rf %q", rf)
	}
	if is != "JR2TTA-10>APZJIG,TCPIP*:;HAMFES   *110233z3510.25N/13652.90E;HAM FES" {
		t.Errorf("object is %q", is)
	}
	p, _ := ParseTNC2(rf)
	if p.Type != "object" || p.Name != "HAMFES" || !p.HasPos || p.Killed {
		t.Errorf("parse %+v", p)
	}
	rf, _ = objectLines(cfg, o, true, now)
	if p, _ := ParseTNC2(rf); !p.Killed {
		t.Errorf("killed object %q", rf)
	}

	it := ObjectDef{Kind: "item", Name: "SHOP1", Lat: 35.12, Lon: 136.93, Sym: `\h`, Comment: "OPEN", Via: "RF", IntervalMin: 30}
	rf, _ = objectLines(cfg, it, false, now)
	if rf != `JR2TTA-10>APZJIG,WIDE1-1:)SHOP1!3507.20N\13655.80EhOPEN` {
		t.Errorf("item %q", rf)
	}
	rf, _ = objectLines(cfg, it, true, now)
	if p, _ := ParseTNC2(rf); p.Type != "item" || !p.Killed || p.Name != "SHOP1" {
		t.Errorf("killed item %q %+v", rf, p)
	}
}

func TestManualObjectPacket(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 5, 0, 0, time.UTC)
	cfg := objCfg()
	o := ObjectDef{Kind: "object", Name: "HAMFES", Sym: "/;", Via: "BOTH", IntervalMin: 30,
		Manual: "JR2TTA-10>APZJIG,WIDE1-1,WIDE2-1:;HAMFES   *010000z3510.25N/13652.90E;edited"}
	rf, is := objectLines(cfg, o, false, now)
	if rf != "JR2TTA-10>APZJIG,WIDE1-1,WIDE2-1:;HAMFES   *110905z3510.25N/13652.90E;edited" {
		t.Errorf("manual rf %q (時刻が更新されること)", rf)
	}
	if !strings.HasPrefix(is, "JR2TTA-10>APZJIG,TCPIP*:;HAMFES") {
		t.Errorf("manual is %q", is)
	}
	rf, _ = objectLines(cfg, o, true, now)
	if p, _ := ParseTNC2(rf); !p.Killed {
		t.Errorf("manual kill %q", rf)
	}
}

func TestValidateObjects(t *testing.T) {
	ok := ObjectDef{Kind: "item", Name: "SHOP1", Sym: `\h`, Via: "RF", IntervalMin: 30, Lat: 35, Lon: 136}
	cfg := objCfg(ok)
	if err := validateObjects(&cfg); err != nil || cfg.Objects[0].ID == "" {
		t.Fatalf("valid rejected: %v", err)
	}
	cases := map[string]ObjectDef{
		"日本語コメント":  {Kind: "item", Name: "SHOP1", Sym: `\h`, Via: "RF", IntervalMin: 30, Comment: "テスト"},
		"短いアイテム名":  {Kind: "item", Name: "AB", Sym: `\h`, Via: "RF", IntervalMin: 30},
		"アイテム名の!":  {Kind: "item", Name: "AB!C", Sym: `\h`, Via: "RF", IntervalMin: 30},
		"他局の手動パケット": {Kind: "object", Name: "X", Sym: "/;", Via: "RF", IntervalMin: 30, Manual: "JA1XXX>APRS:;X        *010000z3510.25N/13652.90E;"},
		"手動に日本語":   {Kind: "object", Name: "X", Sym: "/;", Via: "RF", IntervalMin: 30, Manual: "JR2TTA-10>APZJIG:;X        *010000z3510.25N/13652.90E;テスト"},
		"間隔が短い":    {Kind: "object", Name: "X", Sym: "/;", Via: "RF", IntervalMin: 1},
	}
	for name, o := range cases {
		c := objCfg(o)
		if err := validateObjects(&c); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
	var many []ObjectDef
	for i := 0; i < maxObjects+1; i++ {
		o := ok
		o.Name = "OBJ" + string(rune('A'+i))
		many = append(many, o)
	}
	c := objCfg(many...)
	if err := validateObjects(&c); err == nil {
		t.Error("over limit accepted")
	}
}
