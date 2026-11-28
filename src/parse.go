package main

// APRS パケットの解析 (位置/圧縮位置/Mic-E/オブジェクト/アイテム/メッセージ/ステータス/NMEA/気象)

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
)

type Packet struct {
	Raw  string
	Src  string
	Dst  string
	Path []string
	Info string

	Type string // position, mic-e, object, item, message, status, weather, telemetry, nmea, thirdparty, other
	Name string // オブジェクト/アイテム名

	HasPos   bool
	Lat, Lon float64
	Ambig    int // 位置曖昧さレベル 0〜4 (空白で伏せた桁数)
	SymTable byte
	SymCode  byte
	HasCS    bool
	Course   int
	Speed    float64 // km/h
	HasAlt   bool
	Alt      float64 // m
	Comment  string
	WX       *Weather
	Status   string
	MicEMsg  string
	Killed   bool

	MsgTo    string
	MsgText  string
	MsgID    string
	ReplyAck string
	IsAck    bool
	IsRej    bool
}

// Key はステーション DB 上のキー (オブジェクト/アイテムは名前)。
func (p *Packet) Key() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Src
}

func (p *Packet) Symbol() string {
	if p.SymTable == 0 || p.SymCode == 0 {
		return ""
	}
	return string([]byte{p.SymTable, p.SymCode})
}

func (p *Packet) Header() string {
	var sb strings.Builder
	sb.WriteString(p.Src)
	sb.WriteByte('>')
	sb.WriteString(p.Dst)
	for _, d := range p.Path {
		sb.WriteByte(',')
		sb.WriteString(d)
	}
	return sb.String()
}

func ParseTNC2(raw string) (*Packet, error) {
	colon := strings.IndexByte(raw, ':')
	if colon < 0 {
		return nil, errors.New("no info field")
	}
	hdr, info := raw[:colon], raw[colon+1:]
	gt := strings.IndexByte(hdr, '>')
	if gt <= 0 || gt > 9 {
		return nil, errors.New("bad source")
	}
	parts := strings.Split(hdr[gt+1:], ",")
	if parts[0] == "" {
		return nil, errors.New("bad destination")
	}
	p := &Packet{Raw: raw, Src: hdr[:gt], Dst: parts[0], Path: parts[1:], Info: info, Type: "other"}
	parseInfo(p)
	return p, nil
}

func parseInfo(p *Packet) {
	info := p.Info
	if len(info) == 0 {
		return
	}
	switch info[0] {
	case '!', '=':
		if parsePosition(p, info[1:]) {
			p.Type = "position"
		}
	case '/', '@':
		if len(info) >= 8 && parsePosition(p, info[8:]) {
			p.Type = "position"
		}
	case '`', '\'', 0x1c, 0x1d:
		if parseMicE(p) {
			p.Type = "mic-e"
		}
	case ';':
		if len(info) >= 18 && (info[10] == '*' || info[10] == '_') {
			p.Type = "object"
			p.Name = strings.TrimRight(info[1:10], " ")
			p.Killed = info[10] == '_'
			parsePosition(p, info[18:])
		}
	case ')':
		end := strings.IndexAny(info[1:], "!_")
		if end >= 3 && end <= 9 {
			p.Type = "item"
			p.Name = info[1 : 1+end]
			p.Killed = info[1+end] == '_'
			parsePosition(p, info[2+end:])
		}
	case ':':
		if len(info) >= 11 && info[10] == ':' {
			p.Type = "message"
			parseMessage(p, strings.TrimRight(info[1:10], " "), info[11:])
		}
	case '>':
		p.Type = "status"
		p.Status = info[1:]
	case '_':
		// 位置なし気象: "_" + 日時 MMDDHHMM (8文字) + "cXXXsXXX..."
		p.Type = "weather"
		if len(info) > 9 {
			p.WX, p.Comment = parseWeather(info[9:])
		}
	case '$':
		if parseNMEA(p, info) {
			p.Type = "nmea"
		}
	case '}':
		p.Type = "thirdparty"
	case 'T':
		p.Type = "telemetry"
	}
	if p.Type == "other" {
		// 位置データが先頭以外にある古い形式 (最初の40文字以内の '!')
		if i := strings.IndexByte(info, '!'); i > 0 && i < 40 && parsePosition(p, info[i+1:]) {
			p.Type = "position"
		}
	}
}

func parseMessage(p *Packet, to, text string) {
	p.MsgTo = to
	if len(text) >= 3 && (strings.HasPrefix(text, "ack") || strings.HasPrefix(text, "rej")) {
		id := strings.TrimSpace(text[3:])
		if i := strings.IndexByte(id, '}'); i >= 0 {
			id = id[:i]
		}
		if len(id) > 0 && len(id) <= 5 {
			p.IsAck = text[0] == 'a'
			p.IsRej = text[0] == 'r'
			p.MsgID = id
			return
		}
	}
	if i := strings.LastIndexByte(text, '{'); i >= 0 {
		id := strings.TrimSpace(text[i+1:])
		if j := strings.IndexByte(id, '}'); j >= 0 {
			p.ReplyAck = id[j+1:]
			id = id[:j]
		}
		if len(id) <= 5 {
			p.MsgID = id
			text = text[:i]
		}
	}
	p.MsgText = text
}

func parsePosition(p *Packet, s string) bool {
	if len(s) == 0 {
		return false
	}
	var comment string
	c := s[0]
	if (c >= '0' && c <= '9') || c == ' ' {
		if len(s) < 19 {
			return false
		}
		lat, ok1 := parseLat(s[0:8])
		lon, ok2 := parseLon(s[9:18])
		if !ok1 || !ok2 {
			return false
		}
		p.Lat, p.Lon = lat, lon
		if n := strings.Count(s[0:8], " "); n >= 1 && n <= 4 {
			p.Ambig = n
			p.Lat, p.Lon = ambigCenter(lat, n), ambigCenter(lon, n)
		}
		p.SymTable, p.SymCode = s[8], s[18]
		comment = s[19:]
		if p.SymCode == '_' {
			p.WX, comment = parseWeather(comment)
		} else {
			comment = parseCourseSpeed(p, comment)
		}
	} else {
		if len(s) < 13 {
			return false
		}
		for i := 1; i < 9; i++ {
			if s[i] < '!' || s[i] > '{' {
				return false
			}
		}
		table := s[0]
		if table >= 'a' && table <= 'j' {
			table = table - 'a' + '0'
		}
		p.Lat = 90 - float64(base91(s[1:5]))/380926.0
		p.Lon = -180 + float64(base91(s[5:9]))/190463.0
		p.SymTable, p.SymCode = table, s[9]
		cs, t := s[10], s[12]
		if cs != ' ' && cs >= '!' && cs <= 'z' {
			ct := int(t) - 33
			if ct >= 0 && ct&0x18 == 0x10 {
				p.HasAlt = true
				p.Alt = math.Pow(1.002, float64((int(cs)-33)*91+int(s[11])-33)) * 0.3048
			} else if cs != '{' {
				p.HasCS = true
				p.Course = (int(cs) - 33) * 4
				p.Speed = (math.Pow(1.08, float64(int(s[11])-33)) - 1) * 1.852
			}
		}
		comment = s[13:]
		if p.SymCode == '_' {
			// 圧縮形式の気象局では course/speed が風向/風速を表す
			p.WX, comment = parseWeather(comment)
			if p.HasCS {
				if p.WX == nil {
					p.WX = &Weather{}
				}
				if p.WX.WindDir == nil {
					p.WX.WindDir = iptr(p.Course)
				}
				if p.WX.Wind == nil {
					p.WX.Wind = fptr(p.Speed / 3.6)
				}
				p.HasCS = false
			}
		}
	}
	if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
		return false
	}
	p.HasPos = true
	p.Comment = strings.TrimSpace(extractAltitude(p, comment))
	return true
}

func parseCourseSpeed(p *Packet, comment string) string {
	if len(comment) >= 7 && comment[3] == '/' && allDigits(comment[0:3]) && allDigits(comment[4:7]) {
		if p.SymCode != '_' {
			crs, _ := strconv.Atoi(comment[0:3])
			spd, _ := strconv.Atoi(comment[4:7])
			p.HasCS = true
			p.Course = crs
			p.Speed = float64(spd) * 1.852
		}
		return comment[7:]
	}
	return comment
}

var altRe = regexp.MustCompile(`/A=(-?\d{5,6})`)

func extractAltitude(p *Packet, comment string) string {
	m := altRe.FindStringSubmatchIndex(comment)
	if m == nil {
		return comment
	}
	ft, err := strconv.Atoi(comment[m[2]:m[3]])
	if err == nil {
		p.HasAlt = true
		p.Alt = float64(ft) * 0.3048
	}
	return comment[:m[0]] + comment[m[1]:]
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

func base91(s string) int {
	v := 0
	for i := 0; i < len(s); i++ {
		v = v*91 + int(s[i]) - 33
	}
	return v
}

// ambigSizeDeg は曖昧さレベル (1〜4) ごとの範囲の一辺 (度)。0.1分 / 1分 / 10分 / 1度。
func ambigSizeDeg(level int) float64 {
	return [...]float64{0, 0.1 / 60, 1.0 / 60, 10.0 / 60, 1}[level]
}

// ambigCenter は桁を伏せた座標 (0 埋めで読んだ値 = 範囲の端) を範囲の中心へ補正する。
func ambigCenter(v float64, level int) float64 {
	h := ambigSizeDeg(level) / 2
	if v < 0 {
		return v - h
	}
	return v + h
}

// 位置曖昧さ (空白) は 0 として読み、範囲の中心への補正は ambigCenter で行う
func ambig(s string) string { return strings.ReplaceAll(s, " ", "0") }

func parseLat(s string) (float64, bool) {
	if len(s) != 8 || s[4] != '.' {
		return 0, false
	}
	d, err1 := strconv.Atoi(ambig(s[0:2]))
	m, err2 := strconv.ParseFloat(ambig(s[2:7]), 64)
	if err1 != nil || err2 != nil || d > 90 || m >= 60 {
		return 0, false
	}
	v := float64(d) + m/60
	switch s[7] {
	case 'N', 'n':
	case 'S', 's':
		v = -v
	default:
		return 0, false
	}
	return v, true
}

func parseLon(s string) (float64, bool) {
	if len(s) != 9 || s[5] != '.' {
		return 0, false
	}
	d, err1 := strconv.Atoi(ambig(s[0:3]))
	m, err2 := strconv.ParseFloat(ambig(s[3:8]), 64)
	if err1 != nil || err2 != nil || d > 180 || m >= 60 {
		return 0, false
	}
	v := float64(d) + m/60
	switch s[8] {
	case 'E', 'e':
	case 'W', 'w':
		v = -v
	default:
		return 0, false
	}
	return v, true
}

var micEMessages = [8]string{"Emergency", "Priority", "Special", "Committed", "Returning", "In Service", "En Route", "Off Duty"}

func parseMicE(p *Packet) bool {
	dst := p.Dst
	if i := strings.IndexByte(dst, '-'); i >= 0 {
		dst = dst[:i]
	}
	info := p.Info
	if len(dst) != 6 || len(info) < 9 {
		return false
	}
	// 経度・速度・進路の各バイトは 28〜127 のはず。範囲外 (0x80 以上など) は受信時に化けたパケットなので捨てる
	for i := 1; i <= 6; i++ {
		if info[i] < 28 || info[i] > 127 {
			return false
		}
	}
	var digits [6]int
	msgBits, custom := 0, false
	for i := 0; i < 6; i++ {
		c := dst[i]
		switch {
		case c >= '0' && c <= '9':
			digits[i] = int(c - '0')
		case c >= 'A' && c <= 'J':
			digits[i] = int(c - 'A')
			if i < 3 {
				msgBits |= 1 << (2 - i)
				custom = true
			}
		case c >= 'P' && c <= 'Y':
			digits[i] = int(c - 'P')
			if i < 3 {
				msgBits |= 1 << (2 - i)
			}
		case c == 'K' || c == 'L' || c == 'Z':
			digits[i] = 0
			if i < 3 && c != 'L' {
				msgBits |= 1 << (2 - i)
				custom = custom || c == 'K'
			}
		default:
			return false
		}
	}
	lat := float64(digits[0]*10+digits[1]) + (float64(digits[2]*10+digits[3])+float64(digits[4]*10+digits[5])/100)/60
	if dst[3] < 'P' {
		lat = -lat
	}
	lonOffset := dst[4] >= 'P'
	west := dst[5] >= 'P'

	d := int(info[1]) - 28
	if lonOffset {
		d += 100
	}
	if d >= 180 && d <= 189 {
		d -= 80
	} else if d >= 190 && d <= 199 {
		d -= 190
	}
	m := int(info[2]) - 28
	if m >= 60 {
		m -= 60
	}
	h := int(info[3]) - 28
	if d < 0 || d > 179 || m < 0 || h < 0 || h > 99 {
		return false
	}
	lon := float64(d) + (float64(m)+float64(h)/100)/60
	if west {
		lon = -lon
	}
	if lat < -90 || lat > 90 {
		return false
	}

	sp := (int(info[4])-28)*10 + (int(info[5])-28)/10
	if sp >= 800 {
		sp -= 800
	}
	crs := ((int(info[5])-28)%10)*100 + (int(info[6]) - 28)
	if crs >= 400 {
		crs -= 400
	}
	p.Lat, p.Lon = lat, lon
	// 宛先の末尾から続く K / L / Z (空白) の数が位置曖昧さレベル
	n := 0
	for i := 5; i >= 2 && (dst[i] == 'K' || dst[i] == 'L' || dst[i] == 'Z'); i-- {
		n++
	}
	if n > 0 {
		p.Ambig = n
		// Mic-E の経度は桁が伏せられていないので実値のまま使う (範囲の描画は緯度経度が入る格子)
		p.Lat = ambigCenter(lat, n)
	}
	p.HasPos = true
	if sp >= 0 && crs >= 0 && crs <= 360 {
		p.HasCS = true
		p.Speed = float64(sp) * 1.852
		p.Course = crs
	}
	p.SymCode, p.SymTable = info[7], info[8]
	if custom {
		p.MicEMsg = "Custom-" + strconv.Itoa(7-msgBits)
	} else {
		p.MicEMsg = micEMessages[msgBits]
	}

	comment := info[9:]
	if len(comment) > 0 && (comment[0] == '>' || comment[0] == ']') {
		comment = comment[1:]
	} else if len(comment) > 0 && (comment[0] == '`' || comment[0] == '\'') {
		comment = comment[1:]
	}
	// 高度 "xxx}"
	if len(comment) >= 4 && comment[3] == '}' {
		ok := true
		for i := 0; i < 3; i++ {
			if comment[i] < '!' || comment[i] > '{' {
				ok = false
			}
		}
		if ok {
			p.HasAlt = true
			p.Alt = float64(base91(comment[0:3]) - 10000)
			comment = comment[4:]
		}
	}
	p.Comment = strings.TrimSpace(extractAltitude(p, comment))
	return true
}

func parseNMEA(p *Packet, s string) bool {
	if i := strings.IndexByte(s, '*'); i >= 0 {
		s = s[:i]
	}
	f := strings.Split(s, ",")
	var latS, ns, lonS, ew string
	switch {
	case len(f) >= 9 && strings.HasSuffix(f[0], "RMC"):
		if f[2] != "A" {
			return false
		}
		latS, ns, lonS, ew = f[3], f[4], f[5], f[6]
		if spd, err := strconv.ParseFloat(f[7], 64); err == nil {
			p.HasCS = true
			p.Speed = spd * 1.852
			c, _ := strconv.ParseFloat(f[8], 64)
			p.Course = int(c)
		}
	case len(f) >= 10 && strings.HasSuffix(f[0], "GGA"):
		latS, ns, lonS, ew = f[2], f[3], f[4], f[5]
		if alt, err := strconv.ParseFloat(f[9], 64); err == nil {
			p.HasAlt = true
			p.Alt = alt
		}
	default:
		return false
	}
	lat, err1 := strconv.ParseFloat(latS, 64)
	lon, err2 := strconv.ParseFloat(lonS, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	lat = math.Floor(lat/100) + math.Mod(lat, 100)/60
	lon = math.Floor(lon/100) + math.Mod(lon, 100)/60
	if ns == "S" {
		lat = -lat
	}
	if ew == "W" {
		lon = -lon
	}
	p.Lat, p.Lon, p.HasPos = lat, lon, true
	p.SymTable, p.SymCode = '/', '>'
	return true
}

// displayText は表示用に文字列を UTF-8 化する。UTF-8 として不正な場合は Shift_JIS とみなして変換する。
func displayText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	if out, err := japanese.ShiftJIS.NewDecoder().String(s); err == nil && utf8.ValidString(out) {
		return out
	}
	return strings.ToValidUTF8(s, "?")
}

// baseCall は SSID を除いたコールサイン
func baseCall(c string) string {
	if i := strings.IndexByte(c, '-'); i >= 0 {
		return c[:i]
	}
	return c
}

func sameCall(a, b string) bool {
	a = strings.TrimSuffix(strings.ToUpper(a), "-0")
	b = strings.TrimSuffix(strings.ToUpper(b), "-0")
	return a == b
}

// noRF は call が「RF へ送らない局」リストに該当するかを返す。
// リストはカンマ区切り (正規化済み)。SSID なしは全 SSID、末尾 * は前方一致、それ以外は完全一致。
func noRF(list, call string) bool {
	call = strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(call)), "-0")
	if list == "" || call == "" {
		return false
	}
	for _, e := range strings.Split(list, ",") {
		e = strings.TrimSuffix(strings.TrimSpace(e), "-0")
		switch {
		case e == "":
		case strings.HasSuffix(e, "*"):
			if strings.HasPrefix(call, strings.TrimSuffix(e, "*")) {
				return true
			}
		case strings.Contains(e, "-"):
			if call == e {
				return true
			}
		default:
			if call == e || strings.HasPrefix(call, e+"-") {
				return true
			}
		}
	}
	return false
}
