package main

// デジピータ (RF で受信したパケットを RF へ中継する)

import (
	"log"
	"strconv"
	"strings"
)

// digiPath は中継後のパスを返す。中継しない場合は ok=false。
// path は TNC2 表記 (使用済みの最後の要素に '*')。
//   - 次の未使用要素が自局コール → 自局* に置き換え
//   - WIDE1-1                  → 自局* に置き換え (フィルインデジ)
//   - WIDE2-2 / WIDE2-1 (wide) → 自局*,WIDE2-1 / 自局*,WIDE2* (ホップ数を 1 減らす)
func digiPath(path []string, mycall string, wide bool) ([]string, bool) {
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
		return nil, false
	}
	out := append(used, mid...)
	return append(out, rest...), true
}

// digipeat は RF で受信したパケットを、条件に合えば中継する。
func (a *App) digipeat(p *Packet, cfg Config) {
	if !cfg.DigiEnabled || cfg.Callsign == "" {
		return
	}
	if noRF(cfg.NoRFCalls, p.Src) {
		return
	}
	_, tnc := a.clients()
	if tnc == nil || !tnc.Status().Connected {
		return
	}
	path, ok := digiPath(p.Path, cfg.Callsign, cfg.DigiWide)
	if !ok {
		return
	}
	// 同じパケット (送信元・宛先・内容が同じ) は 30 秒間中継しない
	if !a.dup.Check("digi|" + p.Src + ">" + p.Dst + "|" + p.Info) {
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
