package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
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

func TestMicEAmbiguity(t *testing.T) {
	// 宛先の末尾から続く K/L/Z の数がレベル。SUQL0Z は末尾 Z のみ → 1
	p, _ := ParseTNC2("JA1ABC>SUQL0Z:`c.l+%R/>test")
	if p == nil || !p.HasPos || p.Ambig != 1 {
		t.Fatalf("mic-e ambig 1: %+v", p)
	}
	p, _ = ParseTNC2("JA1ABC>SUQLLZ:`c.l+%R/>test")
	if p == nil || p.Ambig != 3 {
		t.Fatalf("mic-e ambig 3: %+v", p)
	}
}

func TestAmbigStationJSON(t *testing.T) {
	p, _ := ParseTNC2("JA1ABC>APRS:!4903.5 N/07201.7 W-test")
	db := NewStationDB()
	s := db.Update(p, "RF", time.Now(), 10)
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"ambig":1`) {
		t.Errorf("json: %s", b)
	}
}

func TestMicEAmbiguityReal(t *testing.T) {
	// 実機の例: 宛先 S5PUZL = 35°05.  (末尾2桁が空白 → レベル2 = 1分四方)
	p, _ := ParseTNC2("JR2TTA-7>S5PUZL,WIDE1-1,WIDE2-1:`c.l+%R/>Hello")
	if p == nil || !p.HasPos || p.Ambig != 2 {
		t.Fatalf("real mic-e: %+v", p)
	}
	if math.Abs(p.Lat-(35+5.5/60)) > 0.0001 {
		t.Errorf("lat center: %v", p.Lat)
	}
}

func TestNoRF(t *testing.T) {
	list := "JA1ABC,JA2XYZ-9,JA3*"
	for call, want := range map[string]bool{
		"JA1ABC": true, "ja1abc-7": true, "JA1ABCD": false,
		"JA2XYZ-9": true, "JA2XYZ": false, "JA2XYZ-1": false,
		"JA3FOO-5": true, "JA4AAA": false,
	} {
		if got := noRF(list, call); got != want {
			t.Errorf("noRF(%q)=%v want %v", call, got, want)
		}
	}
	if noRF("", "JA1ABC") {
		t.Error("empty list")
	}
}

func TestBeaconPHGSeparator(t *testing.T) {
	cfg := defaultConfig()
	cfg.PHGEnabled, cfg.PHGPower, cfg.PHGHeight, cfg.PHGGain = true, 5, 10, 3
	cfg.Comment = "W1,KNn-N Yokohama 144.640/9600"
	b := beaconBody(cfg)
	if !strings.Contains(b, "PHG2230/W1,KNn-N") {
		t.Errorf("PHG の直後に / がない: %q", b)
	}
	p, _ := ParseTNC2("JA1ABC>APKSXI:" + b)
	if p.Comment != "PHG2230/W1,KNn-N Yokohama 144.640/9600" {
		t.Errorf("comment %q", p.Comment)
	}
	cfg.Comment = ""
	if b := beaconBody(cfg); !strings.HasSuffix(b, "PHG2230") {
		t.Errorf("コメントなしでは / を付けない: %q", b)
	}
	cfg.Comment = strings.Repeat("x", 36) // PHG 7 + 36 = 43 文字ちょうどは / を足さない
	if b := beaconBody(cfg); strings.Contains(b, "PHG2230/") {
		t.Errorf("43文字を超える: %q", b)
	}
	cfg.PHGEnabled = false
	cfg.Comment = "hello"
	if b := beaconBody(cfg); !strings.HasSuffix(b, "hello") || strings.Contains(b, "PHG") {
		t.Errorf("PHGなし %q", b)
	}
}
