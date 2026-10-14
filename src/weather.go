package main

// APRS 気象データの解析。単位は SI に換算して保持する (風速 m/s、気温 ℃、雨量 mm、気圧 hPa)。

import (
	"strconv"
	"strings"
)

type Weather struct {
	WindDir      *int     `json:"windDir,omitempty"`      // 度 (風が吹いてくる方向)
	Wind         *float64 `json:"wind,omitempty"`         // m/s
	Gust         *float64 `json:"gust,omitempty"`         // m/s
	Temp         *float64 `json:"temp,omitempty"`         // ℃
	Rain1h       *float64 `json:"rain1h,omitempty"`       // mm
	Rain24h      *float64 `json:"rain24h,omitempty"`      // mm
	RainMidnight *float64 `json:"rainMidnight,omitempty"` // mm
	Humidity     *int     `json:"humidity,omitempty"`     // %
	Pressure     *float64 `json:"pressure,omitempty"`     // hPa
	Luminosity   *int     `json:"luminosity,omitempty"`   // W/m²
}

const (
	mphToMs   = 0.44704
	inch100Mm = 0.254
)

func fptr(v float64) *float64 { return &v }
func iptr(v int) *int         { return &v }

// wxField は n 文字の数値フィールドを読む。"..." や空白は欠測 (ok=true, has=false)。
// 数値として不正なら ok=false (気象データの終わりとみなす)。
func wxField(s string, n int) (v float64, has, ok bool) {
	if len(s) < n {
		return 0, false, false
	}
	f := s[:n]
	for i := 0; i < n; i++ {
		c := f[i]
		if !(c >= '0' && c <= '9') && c != '.' && c != ' ' && c != '-' {
			return 0, false, false
		}
	}
	t := strings.TrimSpace(f)
	if t == "" || strings.Trim(t, ".") == "" {
		return 0, false, true
	}
	x, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false, true
	}
	return x, true, true
}

// parseWeather は気象データ部を解析し、残り (ソフトウェア種別やコメント) を返す。
func parseWeather(s string) (*Weather, string) {
	w := &Weather{}
	found := false
	i := 0

	// 位置付き気象: 先頭 "ddd/sss" = 風向/風速
	if len(s) >= 7 && s[3] == '/' {
		d, hd, ok1 := wxField(s[0:3], 3)
		sp, hs, ok2 := wxField(s[4:7], 3)
		if ok1 && ok2 {
			if hd {
				w.WindDir = iptr(int(d))
			}
			if hs {
				w.Wind = fptr(sp * mphToMs)
			}
			found = true
			i = 7
		}
	}

	afterC := false
	for i < len(s) {
		key := s[i]
		width := 3
		switch key {
		case 'c', 's', 'g', 't', 'r', 'p', 'P', 'L', 'l', '#':
		case 'h':
			width = 2
		case 'b':
			width = 5
		default:
			return finishWX(w, found, s[i:])
		}
		v, has, ok := wxField(s[i+1:], width)
		if !ok {
			return finishWX(w, found, s[i:])
		}
		i += 1 + width
		found = true
		if !has {
			afterC = key == 'c'
			continue
		}
		switch key {
		case 'c':
			w.WindDir = iptr(int(v))
		case 's':
			if afterC { // 位置なし気象の "cXXXsXXX" は風速。それ以外の s は降雪量なので無視
				w.Wind = fptr(v * mphToMs)
			}
		case 'g':
			w.Gust = fptr(v * mphToMs)
		case 't':
			w.Temp = fptr((v - 32) * 5 / 9)
		case 'r':
			w.Rain1h = fptr(v * inch100Mm)
		case 'p':
			w.Rain24h = fptr(v * inch100Mm)
		case 'P':
			w.RainMidnight = fptr(v * inch100Mm)
		case 'h':
			if v == 0 {
				v = 100
			}
			w.Humidity = iptr(int(v))
		case 'b':
			w.Pressure = fptr(v / 10)
		case 'L':
			w.Luminosity = iptr(int(v))
		case 'l':
			w.Luminosity = iptr(int(v) + 1000)
		}
		afterC = key == 'c'
	}
	return finishWX(w, found, "")
}

func finishWX(w *Weather, found bool, rest string) (*Weather, string) {
	if !found {
		return nil, rest
	}
	return w, strings.TrimSpace(rest)
}
