package main

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.001 }

func TestUncompressed(t *testing.T) {
	p, err := ParseTNC2("JA1ABC-9>APRS,WIDE1-1*,WIDE2-1:=3540.87N/13945.66E>090/036/A=000123 test")
	if err != nil || !p.HasPos {
		t.Fatalf("parse failed: %v %+v", err, p)
	}
	if !near(p.Lat, 35+40.87/60) || !near(p.Lon, 139+45.66/60) || p.Symbol() != "/>" {
		t.Errorf("pos %+v", p)
	}
	if !p.HasCS || p.Course != 90 || !near(p.Speed, 36*1.852) || !p.HasAlt || !near(p.Alt, 123*0.3048) || p.Comment != "test" {
		t.Errorf("ext %+v", p)
	}
}

func TestCompressed(t *testing.T) {
	// APRS 仕様書の例
	p, _ := ParseTNC2("N0CALL>APRS:!/5L!!<*e7>7P[")
	if !p.HasPos || !near(p.Lat, 49.5) || !near(p.Lon, -72.75) || p.Course != 88 || math.Abs(p.Speed-36.2*1.852) > 1 {
		t.Errorf("compressed %+v", p)
	}
}

func TestMicE(t *testing.T) {
	// 33°25.64'N 112°07.74'W (宛先5文字目 V = 経度 +100)
	p, _ := ParseTNC2("N0CALL>S32UVT:`(_fn\"Oj/]comment")
	if !p.HasPos || !near(p.Lat, 33+25.64/60) || !near(p.Lon, -(112+7.74/60)) {
		t.Errorf("mic-e %+v", p)
	}
	if p.SymCode != 'j' || p.SymTable != '/' {
		t.Errorf("mic-e symbol %c%c", p.SymTable, p.SymCode)
	}
}

func TestMessage(t *testing.T) {
	p, _ := ParseTNC2("JA1ABC>APRS::JA1XYZ-10:hello{12")
	if p.Type != "message" || p.MsgTo != "JA1XYZ-10" || p.MsgText != "hello" || p.MsgID != "12" {
		t.Errorf("msg %+v", p)
	}
	p, _ = ParseTNC2("JA1ABC>APRS::JA1XYZ   :ack12")
	if !p.IsAck || p.MsgID != "12" || p.MsgTo != "JA1XYZ" {
		t.Errorf("ack %+v", p)
	}
	p, _ = ParseTNC2("JA1ABC>APRS::JA1XYZ   :hi{AB}CD")
	if p.MsgID != "AB" || p.ReplyAck != "CD" || p.MsgText != "hi" {
		t.Errorf("replyack %+v", p)
	}
}

func TestObject(t *testing.T) {
	p, _ := ParseTNC2("JA1ABC>APRS:;LEADER   *092345z4903.50N/07201.75W>088/036")
	if p.Type != "object" || p.Name != "LEADER" || !p.HasPos || p.Key() != "LEADER" {
		t.Errorf("object %+v", p)
	}
}

func TestAX25RoundTrip(t *testing.T) {
	in := "JA1ABC-9>APZJIG,JA1DIG-1*,WIDE2-1:=3540.87N/13945.66E>test"
	f, err := tnc2ToAX25(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ax25ToTNC2(f)
	if err != nil || out != in {
		t.Errorf("roundtrip %q -> %q (%v)", in, out, err)
	}
	var d kissDecoder
	frames := d.Feed(kissEncode(0, append([]byte{kissFEND, kissFESC}, f...)))
	if len(frames) != 1 || frames[0][0] != 0 || frames[0][1] != kissFEND || frames[0][2] != kissFESC {
		t.Errorf("kiss escape failed")
	}
}

func TestBeaconFormat(t *testing.T) {
	cfg := defaultConfig()
	cfg.Lat, cfg.Lon = 35.6812, 139.7671
	b := beaconBody(cfg)
	p, _ := ParseTNC2("JA1ABC>APZJIG:" + b)
	if !p.HasPos || math.Abs(p.Lat-cfg.Lat) > 0.0002 || math.Abs(p.Lon-cfg.Lon) > 0.0002 || p.Symbol() != "I&" {
		t.Errorf("beacon %q -> %+v", b, p)
	}
	if formatLat(-0.99999) != "0060.00S" && formatLat(-0.99999) != "0100.00S" {
		t.Errorf("rounding %s", formatLat(-0.99999))
	}
}

func TestShiftJIS(t *testing.T) {
	// "テスト" in Shift_JIS
	if s := displayText("\x83\x65\x83\x58\x83\x67"); s != "テスト" {
		t.Errorf("sjis %q", s)
	}
}

func TestGateRules(t *testing.T) {
	if !pathHas([]string{"WIDE1-1", "TCPIP*"}, "TCPIP") || !pathHas([]string{"NOGATE"}, "NOGATE") || pathHas([]string{"WIDE1-1"}, "TCPIP") {
		t.Error("pathHas")
	}
}

func TestPositionAmbiguity(t *testing.T) {
	p, _ := ParseTNC2("JA1ABC>APRS:!4903.5 N/07201.7 W-test")
	if p == nil || !p.HasPos || p.Ambig != 1 {
		t.Fatalf("ambig level: %+v", p)
	}
	p, _ = ParseTNC2("JA1ABC>APRS:!49  .  N/072  .  W-test")
	if p == nil || p.Ambig != 4 {
		t.Fatalf("ambig 4: %+v", p)
	}
	if p.Lat < 49.49 || p.Lat > 49.51 || p.Lon > -72.49 || p.Lon < -72.51 {
		t.Errorf("center: %v %v", p.Lat, p.Lon)
	}
	p, _ = ParseTNC2("JA1ABC>APRS:!4903.50N/07201.75W-test")
	if p == nil || p.Ambig != 0 {
		t.Errorf("no ambig: %+v", p)
	}
}
