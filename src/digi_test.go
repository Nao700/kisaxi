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
		// UIflood: 自局コールは入れず残りホップだけ減らす
		{"KN3-3", false, "KN3-2"},
		{"KN1-1", false, "KN1*"}, // 最終ホップ
		{"KN3-1", false, "KN3*"},
		{"KN3-3,WIDE2-1", false, "KN3-2,WIDE2-1"},
		{"JA2DIG*,NS2-2", false, "JA2DIG,NS2-1"},
		{"AC3-3", false, ""}, // 未登録のエイリアス
		{"KN3-4", false, ""}, // N > n は不正
		{"KN8-8", false, ""},
		{"KN3-0", false, ""},
		{"KN", false, ""},
		{"KN3-3*", false, ""}, // 使い切り
		// UItrace: 自局コールを残す
		{"TRACE3-3", false, "JR2TTA-10*,TRACE3-2"},
		{"TRACE2-1", false, "JR2TTA-10,TRACE2*"},
		{"JA2DIG*,TRACE3-3", false, "JA2DIG,JR2TTA-10*,TRACE3-2"},
		{"JR2TTA-10*,TRACE3-2", false, ""}, // 自局が中継済み (ループ防止)
	}
	for _, c := range cases {
		var path []string
		if c.in != "" {
			path = strings.Split(c.in, ",")
		}
		got, ok := digiPath(path, me, c.wide, []string{"KN", "NS"}, []string{"TRACE"})
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
	path, _ := digiPath([]string{"WIDE1-1", "WIDE2-1"}, "JR2TTA-10", false, nil, nil)
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
