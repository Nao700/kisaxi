package main

// APRS 気象データの解析。単位は SI に換算して保持する (風速 m/s、気温 ℃、雨量 mm、気圧 hPa)。

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
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

// ---------- 送信用の組み立て (parseWeather の逆変換) ----------

func roundClamp(v float64, lo, hi int) int {
	n := int(math.Round(v))
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// wxData は気象データ部 "ddd/sssgGGGtTTTrRRRpPPPPPPhHHbBBBBBLLLL" を作る。
// 風向・風速・突風・気温は必須項目なので、欠測のときは "..." にする。それ以外は欠測なら省く。
func wxData(w *Weather) string {
	var b strings.Builder
	if w.WindDir != nil && w.Wind != nil {
		fmt.Fprintf(&b, "%03d/%03d", roundClamp(float64(*w.WindDir), 1, 360), roundClamp(*w.Wind/mphToMs, 0, 999))
	} else {
		b.WriteString(".../...")
	}
	if w.Gust != nil {
		fmt.Fprintf(&b, "g%03d", roundClamp(*w.Gust/mphToMs, 0, 999))
	} else {
		b.WriteString("g...")
	}
	if w.Temp != nil {
		fmt.Fprintf(&b, "t%03d", roundClamp(*w.Temp*9/5+32, -99, 999))
	} else {
		b.WriteString("t...")
	}
	if w.Rain1h != nil {
		fmt.Fprintf(&b, "r%03d", roundClamp(*w.Rain1h/inch100Mm, 0, 999))
	}
	if w.Rain24h != nil {
		fmt.Fprintf(&b, "p%03d", roundClamp(*w.Rain24h/inch100Mm, 0, 999))
	}
	if w.RainMidnight != nil {
		fmt.Fprintf(&b, "P%03d", roundClamp(*w.RainMidnight/inch100Mm, 0, 999))
	}
	if w.Humidity != nil {
		h := roundClamp(float64(*w.Humidity), 1, 100)
		fmt.Fprintf(&b, "h%02d", h%100) // 100% は "00"
	}
	if w.Pressure != nil {
		fmt.Fprintf(&b, "b%05d", roundClamp(*w.Pressure*10, 0, 99999))
	}
	if w.Luminosity != nil {
		if l := *w.Luminosity; l < 1000 {
			fmt.Fprintf(&b, "L%03d", roundClamp(float64(l), 0, 999))
		} else {
			fmt.Fprintf(&b, "l%03d", roundClamp(float64(l-1000), 0, 999))
		}
	}
	return b.String()
}

// wxPacketBody は位置・時刻付きの気象パケット本文を作る。
// 例: @151230z3540.87N/13946.03E_220/004g009t064r000p012P004h72b10132L450eKSXI
func wxPacketBody(lat, lon float64, w *Weather, now time.Time) string {
	u := now.UTC()
	return fmt.Sprintf("@%02d%02d%02dz%s/%s_%seKSXI", u.Day(), u.Hour(), u.Minute(), formatLat(lat), formatLon(lon), wxData(w))
}
