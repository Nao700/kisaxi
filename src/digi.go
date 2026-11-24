package main

// デジピータ (RF で受信したパケットを RF へ中継する)

import (
	"errors"
	"log"
	"strconv"
	"strings"
	"time"
)

// digiPath は中継後のパスを返す。中継しない場合は ok=false。
// path は TNC2 表記 (使用済みの最後の要素に '*')。
//   - 次の未使用要素が自局コール → 自局* に置き換え
//   - WIDE1-1                  → 自局* に置き換え (フィルインデジ)
//   - WIDE2-2 / WIDE2-1 (wide) → 自局*,WIDE2-1 / 自局*,WIDE2* (ホップ数を 1 減らす)
func digiPath(path []string, mycall string, wide bool, aliases []string) ([]string, bool) {
	next := 0
	for i, d := range path {
		if strings.HasSuffix(d, "*") {
			next = i + 1
		}
	}
	if next >= len(path) {
		return nil, false
	}
	// 既に自局が中継済みならループ防止のため中継しない
	for _, d := range path[:next] {
		if sameCall(strings.TrimSuffix(d, "*"), mycall) {
			return nil, false
		}
	}
	used := make([]string, 0, len(path)+1)
	for _, d := range path[:next] {
		used = append(used, strings.TrimSuffix(d, "*"))
	}
	rest := path[next+1:]
	h := strings.ToUpper(path[next])

	var mid []string
	switch {
	case sameCall(h, mycall), h == "WIDE1-1":
		mid = []string{mycall + "*"}
	case wide && (h == "WIDE2-2" || h == "WIDE2-1"):
		n, _ := strconv.Atoi(h[6:])
		if n-1 > 0 {
			mid = []string{mycall + "*", "WIDE2-" + strconv.Itoa(n-1)}
		} else {
			mid = []string{mycall, "WIDE2*"}
		}
		if len(used)+len(mid)+len(rest) > 8 {
			return nil, false
		}
	default:
		// 地域エイリアス (KNn-N) / UItrace 風 (TRACEn-N): 自局コールを残して残りホップを 1 減らす
		name, n, ok := aliasHop(h, aliases)
		if !ok {
			return nil, false
		}
		if n-1 > 0 {
			mid = []string{mycall + "*", name + "-" + strconv.Itoa(n-1)}
		} else {
			mid = []string{mycall, name + "*"}
		}
		if len(used)+len(mid)+len(rest) > 8 {
			return nil, false
		}
	}
	out := append(used, mid...)
	return append(out, rest...), true
}

// splitAliases は設定文字列をエイリアスの一覧にする。
func splitAliases(s string) []string {
	var out []string
	for _, a := range strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool { return r == ',' || r == ' ' || r == '、' || r == '\n' }) {
		out = append(out, a)
	}
	return out
}

// checkAlias は地域エイリアスの書式を検証する (英字 1〜5 文字。WIDE などの予約語は不可)。
func checkAlias(a string) error {
	if len(a) < 1 || len(a) > 5 {
		return errors.New("地域エイリアスは英字 1〜5 文字で指定してください: " + a)
	}
	for _, r := range a {
		if r < 'A' || r > 'Z' {
			return errors.New("地域エイリアスは英字のみで指定してください (例: KN, NS, AC): " + a)
		}
	}
	switch a {
	case "WIDE", "TCPIP", "TCPXX", "NOGATE", "RFONLY", "RELAY", "ECHO", "GATE", "RFO":
		return errors.New("地域エイリアスに使えない名前です: " + a)
	}
	return nil
}

// aliasHop は "KN3-3" のような要素が登録エイリアスの n-N 形式なら、エイリアス名+n と残りホップ N を返す。
// n は 1〜7、N は 1〜n。
func aliasHop(h string, aliases []string) (name string, remain int, ok bool) {
	i := strings.IndexByte(h, '-')
	if i < 3 || i > len(h)-2 || len(h)-i != 2 {
		return "", 0, false
	}
	base := h[:i]
	nn, err1 := strconv.Atoi(h[i+1:])
	tot, err2 := strconv.Atoi(base[len(base)-1:])
	if err1 != nil || err2 != nil || tot < 1 || tot > 7 || nn < 1 || nn > tot {
		return "", 0, false
	}
	for _, a := range aliases {
		if base[:len(base)-1] == a {
			return base, nn, true
		}
	}
	return "", 0, false
}

// splitComments はコメント絞り込みの設定文字列を分割する (カンマ・読点・改行区切り)。
func splitComments(s string) []string {
	var out []string
	for _, c := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '、' || r == '\n' }) {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// digiSelected は中継対象の絞り込み設定に合うかを返す。
// 絞り込み (局指定・緯度経度の範囲・コメント) が 1 つも設定されていなければ常に true。
// 設定されている場合は、どれか 1 つでも合えば true (OR)。位置を持たないパケットは範囲にもコメントにも合わない。
func digiSelected(p *Packet, cfg Config) bool {
	if cfg.DigiOnlyCalls == "" && !cfg.DigiAreaOn && cfg.DigiOnlyComments == "" {
		return true
	}
	if cfg.DigiOnlyCalls != "" && noRF(cfg.DigiOnlyCalls, p.Src) {
		return true
	}
	if cfg.DigiAreaOn && p.HasPos &&
		p.Lat >= cfg.DigiLatMin && p.Lat <= cfg.DigiLatMax &&
		p.Lon >= cfg.DigiLonMin && p.Lon <= cfg.DigiLonMax {
		return true
	}
	if cfg.DigiOnlyComments != "" && p.HasPos {
		c := strings.ToUpper(p.Comment)
		for _, w := range splitComments(cfg.DigiOnlyComments) {
			if strings.Contains(c, strings.ToUpper(w)) {
				return true
			}
		}
	}
	return false
}

// digipeat は RF で受信したパケットを、条件に合えば中継する。
func (a *App) digipeat(p *Packet, cfg Config) {
	if !cfg.DigiEnabled || cfg.Callsign == "" {
		return
	}
	_, tnc := a.clients()
	if tnc == nil || !tnc.Status().Connected {
		return
	}
	if !digiSelected(p, cfg) {
		return
	}
	aliases := splitAliases(cfg.DigiAliases)
	path, ok := digiPath(p.Path, cfg.Callsign, cfg.DigiWide, aliases)
	if !ok {
		return
	}
	// 同じパケット (送信元・宛先・内容が同じ) は 30 秒間中継しない
	if !a.dup.Check("digi|" + p.Src + ">" + p.Dst + "|" + p.Info) {
		return
	}
	if isAliasRelay(p.Path, cfg.Callsign, aliases) && !a.digiAliasAllow(cfg) {
		return
	}
	line := p.Src + ">" + p.Dst + "," + strings.Join(path, ",") + ":" + p.Info
	if err := tnc.Send(line); err != nil {
		log.Printf("デジピート送信失敗: %v", err)
		return
	}
	a.smu.Lock()
	a.stats.Digi++
	a.smu.Unlock()
	a.emitPacket("DIGI", line)
}

// isAliasRelay は次に処理する要素が地域エイリアスかどうかを返す。
func isAliasRelay(path []string, mycall string, aliases []string) bool {
	next := 0
	for i, d := range path {
		if strings.HasSuffix(d, "*") {
			next = i + 1
		}
	}
	if next >= len(path) {
		return false
	}
	_, _, ok := aliasHop(strings.ToUpper(path[next]), aliases)
	return ok
}

// digiAliasAllow は地域エイリアス中継のレート制限 (件/分) を確認し、枠を 1 つ使う。
func (a *App) digiAliasAllow(cfg Config) bool {
	a.hmu.Lock()
	defer a.hmu.Unlock()
	now := time.Now()
	kept := a.digiAliasLog[:0]
	for _, t := range a.digiAliasLog {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	a.digiAliasLog = kept
	if len(kept) >= cfg.DigiAliasPerMin {
		return false
	}
	a.digiAliasLog = append(a.digiAliasLog, now)
	return true
}
