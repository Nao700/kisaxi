package main

import "testing"

func TestDigiSelected(t *testing.T) {
	pos := &Packet{Src: "JA1ABC-9", HasPos: true, Lat: 35.45, Lon: 139.63, Comment: "Yokohama W1,KNn-N"}
	nopos := &Packet{Src: "JA1ABC-9", Comment: "Yokohama"}
	cases := []struct {
		name string
		cfg  Config
		p    *Packet
		want bool
	}{
		{"条件なしは送出しない", Config{}, pos, false},
		{"Mic-E の In Service", Config{IStoRFMicE: "In Service"}, &Packet{Src: "JA9ZZZ", HasPos: true, MicEMsg: "In Service"}, true},
		{"Mic-E 別の状態は外れ", Config{IStoRFMicE: "In Service"}, &Packet{Src: "JA9ZZZ", HasPos: true, MicEMsg: "Off Duty"}, false},
		{"Mic-E でない位置は外れ", Config{IStoRFMicE: "In Service"}, pos, false},
		{"OR: 局が外れても Mic-E の状態が合えば", Config{IStoRFCalls: "JA1ABC", IStoRFMicE: "En Route"}, &Packet{Src: "JA9ZZZ", HasPos: true, MicEMsg: "En Route"}, true},
		{"局指定 一致", Config{IStoRFCalls: "JA1ABC"}, pos, true},
		{"局指定 SSID 完全一致 外れ", Config{IStoRFCalls: "JA1ABC-7"}, pos, false},
		{"局指定 前方一致", Config{IStoRFCalls: "JA1*"}, nopos, true},
	}
	for _, c := range cases {
		if got := isRFSelected(c.p, c.cfg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
