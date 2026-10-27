package main

import (
	"math"
	"strconv"
)

// PHG (Power-Height-Gain-Directivity) データ拡張。APRS101 第 7 章。
// 各値は 1 桁のコードに丸めて送る。通達範囲の計算と地図表示は web/app.js 側で行う。

// phgCode は送信出力 (W)・アンテナ高 (m, 平均地表高)・利得 (dBi)・指向 (0=無指向, 45〜360 度) を "PHGphgd" にする。
func phgCode(powerW, heightM, gainDB float64, dir int) string {
	p := clampDigit(int(math.Round(math.Sqrt(math.Max(powerW, 0)))))
	h := 0
	if ft := heightM / 0.3048; ft > 10 {
		h = clampDigit(int(math.Round(math.Log2(ft / 10))))
	}
	g := clampDigit(int(math.Round(gainDB)))
	d := 0
	if dir > 0 {
		d = (dir + 22) / 45
		if d < 1 {
			d = 1
		}
		if d > 8 {
			d = 8
		}
	}
	return "PHG" + strconv.Itoa(p) + strconv.Itoa(h) + strconv.Itoa(g) + strconv.Itoa(d)
}

func clampDigit(v int) int {
	if v < 0 {
		return 0
	}
	if v > 9 {
		return 9
	}
	return v
}
