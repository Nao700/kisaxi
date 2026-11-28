package main

import (
	"testing"
	"time"
)

func TestCWOPPass(t *testing.T) {
	c := defaultConfig()
	c.Passcode = "12345"
	if got := cwopPass(c, "CW1234"); got != "-1" {
		t.Errorf("CW ID: %q", got)
	}
	if got := cwopPass(c, "JR2TTA-13"); got != "12345" {
		t.Errorf("ham: %q", got)
	}
	c.WXCWOPPass = "999"
	if got := cwopPass(c, "CW1234"); got != "999" {
		t.Errorf("explicit: %q", got)
	}
}

func TestCWOPIDAndLine(t *testing.T) {
	c := defaultConfig()
	c.Callsign = "JR2TTA"
	if got := cwopID(c); got != "JR2TTA-13" {
		t.Errorf("default id: %q", got)
	}
	c.WXCWOPID = " cw1234 "
	if got := cwopID(c); got != "CW1234" {
		t.Errorf("id: %q", got)
	}
	if got := cwopLine("CW1234", "@x"); got != "CW1234>APKSXI,TCPIP*:@x" {
		t.Errorf("line: %q", got)
	}
	if got := cwopServer(c); got != "cwop.aprs.net:14580" {
		t.Errorf("server: %q", got)
	}
}

// CWOP 経由で APRS-IS に入ってくる -13 の気象局が、局として登録され位置・気象が取れること。
func TestCWOPStations13Parsed(t *testing.T) {
	lines := []string{
		"JA1ABC-13>APRS,TCPXX*,qAX,CWOP-5:@271234z3540.00N/13945.00E_090/002g005t060r000p000P000h50b10130",
		"JA1ABC-13>APRS,TCPIP*,qAC,CWOP-3:!3540.00N/13945.00E_090/002g005t060r000p000P000h50b10130",
		"JA1ABC-13>APRS,TCPIP*,qAU,CWOP-3:=3540.00N/13945.00E_090/002g005t060r000p000P000h50b10130eCumulusMX",
	}
	cfg := defaultConfig()
	cfg.Callsign = "JR2TTA"
	cfg.WXEnabled = true
	db := NewStationDB()
	for _, l := range lines {
		p, err := ParseTNC2(l)
		if err != nil {
			t.Fatal(err)
		}
		if isOwnCall(p.Src, cfg) {
			t.Errorf("他局 %s が自局扱いになっている", p.Src)
		}
		s := db.Update(p, "IS", time.Now(), 10)
		if !s.HasPos || s.WX == nil {
			t.Errorf("%s: pos=%v wx=%v", l, s.HasPos, s.WX != nil)
		}
	}
}
