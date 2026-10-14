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
		{"WIDE2-1", false, ""},                         // 広域は設定オフなら中継しない
		{"WIDE2-2", true, "JR2TTA-10*,WIDE2-1"},        // ホップ数を 1 減らす
		{"JA2DIG*,WIDE2-1", true, "JA2DIG,JR2TTA-10,WIDE2*"},
		{"WIDE1-1*,WIDE2-1", false, ""},                // 次は WIDE2-1 (設定オフ)
		{"JA2DIG,WIDE1*,WIDE2-1", true, "JA2DIG,WIDE1,JR2TTA-10,WIDE2*"},
		{"JR2TTA-10*,WIDE2-1", true, ""},               // 自局が中継済み (ループ防止)
		{"WIDE2-1*", true, ""},                         // パスを使い切っている
		{"", false, ""},                                // パスなし (直接)
		{"WIDE3-3", true, ""},                          // WIDE3 以上は対象外
		{"TCPIP*", false, ""},
	}
	for _, c := range cases {
		var path []string
		if c.in != "" {
			path = strings.Split(c.in, ",")
		}
		got, ok := digiPath(path, me, c.wide)
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
	path, _ := digiPath([]string{"WIDE1-1", "WIDE2-1"}, "JR2TTA-10", false)
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
