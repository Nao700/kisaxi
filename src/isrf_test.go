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
		{"局指定 一致", Config{IStoRFCalls: "JA1ABC"}, pos, true},
		{"局指定 SSID 完全一致 外れ", Config{IStoRFCalls: "JA1ABC-7"}, pos, false},
		{"局指定 前方一致", Config{IStoRFCalls: "JA1*"}, nopos, true},
		{"コメント 一致 (大小文字無視)", Config{IStoRFComments: "yokohama"}, pos, true},
		{"コメント 不一致", Config{IStoRFComments: "nagasaki"}, pos, false},
		{"OR: 局が外れでもコメントが合えば", Config{IStoRFCalls: "JA9ZZZ", IStoRFComments: "KNn"}, pos, true},
	}
	for _, c := range cases {
		if got := isRFSelected(c.p, c.cfg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
