package main

import (
	"strings"
	"testing"
)

func TestDigiPath(t *testing.T) {
	const me = "JR2TTA-10"
	cases := []struct {
		in   string
		wide bool
		want string // "" = 中継しない
	}{
		{"WIDE1-1,WIDE2-1", false, "JR2TTA-10*,WIDE2-1"},
		{"WIDE1-1", false, "JR2TTA-10*"},
		{"JR2TTA-10", false, "JR2TTA-10*"},
		{"JA2DIG*,JR2TTA-10", false, "JA2DIG,JR2TTA-10*"},
		{"WIDE2-1", false, ""},                  // 広域は設定オフなら中継しない
		{"WIDE2-2", true, "JR2TTA-10*,WIDE2-1"}, // ホップ数を 1 減らす
		{"JA2DIG*,WIDE2-1", true, "JA2DIG,JR2TTA-10,WIDE2*"},
		{"WIDE1-1*,WIDE2-1", false, ""}, // 次は WIDE2-1 (設定オフ)
		{"JA2DIG,WIDE1*,WIDE2-1", true, "JA2DIG,WIDE1,JR2TTA-10,WIDE2*"},
		{"JR2TTA-10*,WIDE2-1", true, ""}, // 自局が中継済み (ループ防止)
		{"WIDE2-1*", true, ""},           // パスを使い切っている
		{"", false, ""},                  // パスなし (直接)
		{"WIDE3-3", true, ""},            // WIDE3 以上は対象外
		{"TCPIP*", false, ""},
		{"KN1-1", false, "JR2TTA-10,KN1*"}, // 地域エイリアス: 最終ホップ
		{"KN3-3", false, "JR2TTA-10*,KN3-2"},
		{"KN3-3,WIDE2-1", false, "JR2TTA-10*,KN3-2,WIDE2-1"},
		{"KN3-1", false, "JR2TTA-10,KN3*"},
		{"NS2-2", false, "JR2TTA-10*,NS2-1"},
		{"AC3-3", false, ""}, // 未登録のエイリアス
		{"KN3-4", false, ""}, // N > n は不正
		{"KN8-8", false, ""},
		{"KN3-0", false, ""},
		{"KN", false, ""},
		{"KN3-3*", false, ""},           // 使い切り
		{"JR2TTA-10*,KN3-2", false, ""}, // 自局が中継済み (ループ防止)
	}
	for _, c := range cases {
		var path []string
		if c.in != "" {
			path = strings.Split(c.in, ",")
		}
		got, ok := digiPath(path, me, c.wide, []string{"KN", "NS"})
		gs := ""
		if ok {
			gs = strings.Join(got, ",")
		}
		if gs != c.want {
			t.Errorf("digiPath(%q, wide=%v) = %q, want %q", c.in, c.wide, gs, c.want)
		}
	}
}

func TestDigiFrameHBits(t *testing.T) {
	// 中継後のフレームで、自局までの要素に H ビット (中継済み) が立つこと
	path, _ := digiPath([]string{"WIDE1-1", "WIDE2-1"}, "JR2TTA-10", false, nil)
	line := "JR2TTA-7>APK004," + strings.Join(path, ",") + ":>test"
	f, err := tnc2ToAX25(line)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := ax25ToTNC2(f)
	if back != line {
		t.Errorf("roundtrip %q -> %q", line, back)
	}
}

func TestDigiSelected(t *testing.T) {
	pos := &Packet{Src: "JA1ABC-9", HasPos: true, Lat: 35.45, Lon: 139.63, Comment: "Yokohama W1,KNn-N"}
	nopos := &Packet{Src: "JA1ABC-9", Comment: "Yokohama"}
	cases := []struct {
		name string
		cfg  Config
		p    *Packet
		want bool
	}{
		{"絞り込みなし", Config{}, pos, true},
		{"局指定 一致", Config{DigiOnlyCalls: "JA1ABC"}, pos, true},
		{"局指定 SSID 完全一致 外れ", Config{DigiOnlyCalls: "JA1ABC-7"}, pos, false},
		{"局指定 前方一致", Config{DigiOnlyCalls: "JA1*"}, nopos, true},
		{"範囲内", Config{DigiAreaOn: true, DigiLatMin: 35, DigiLatMax: 36, DigiLonMin: 139, DigiLonMax: 140}, pos, true},
		{"範囲外", Config{DigiAreaOn: true, DigiLatMin: 36, DigiLatMax: 37, DigiLonMin: 139, DigiLonMax: 140}, pos, false},
		{"範囲 位置なし", Config{DigiAreaOn: true, DigiLatMin: 35, DigiLatMax: 36, DigiLonMin: 139, DigiLonMax: 140}, nopos, false},
		{"コメント 一致 (大小文字無視)", Config{DigiOnlyComments: "yokohama"}, pos, true},
		{"コメント 不一致", Config{DigiOnlyComments: "nagasaki"}, pos, false},
		{"OR: 局が外れでもコメントが合えば", Config{DigiOnlyCalls: "JA9ZZZ", DigiOnlyComments: "KNn"}, pos, true},
	}
	for _, c := range cases {
		if got := digiSelected(c.p, c.cfg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
