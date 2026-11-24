package main

// APRS-IS → RF の送出条件 (コールサイン名指し)

import (
	"log"
	"time"
)

// isRFSelected は APRS-IS から受けたパケットを RF へ送出する条件に合うかを返す。
// 指定した局 (コールサイン名指し) のパケットなら true。
// 何も設定されていなければ false (位置情報は送出しない)。
func isRFSelected(p *Packet, cfg Config) bool {
	if cfg.IStoRFCalls != "" && noRF(cfg.IStoRFCalls, p.Src) {
		return true
	}
	return false
}

// gatePosToRF は APRS-IS から受けた位置情報のうち、送出条件に合う局のものを RF へ送る。
func (a *App) gatePosToRF(p *Packet, cfg Config) {
	if !p.HasPos || p.Type == "message" || p.Killed {
		return
	}
	if !isRFSelected(p, cfg) {
		return
	}
	if noRF(cfg.NoRFCalls, p.Src) || pathHas(p.Path, "TCPXX", "NOGATE", "RFONLY") {
		return
	}
	_, tnc := a.clients()
	if tnc == nil || !tnc.Status().Connected {
		return
	}
	// 既に RF で聞こえている局は送らない (メッセージのゲートと同じ範囲)
	if a.heardOnRF(p.Src, time.Duration(cfg.LocalHeardMin)*time.Minute) {
		return
	}
	if !a.dup.Check("rfpos|" + p.Src + "|" + p.Info) {
		return
	}
	if !a.rfGateAllow(cfg) {
		return
	}
	inner := p.Src + ">" + p.Dst + ",TCPIP," + cfg.Callsign + "*:" + p.Info
	line := cfg.Callsign + ">" + toCall + pathSuffix(cfg.IStoRFPath) + ":}" + inner
	if err := tnc.Send(line); err != nil {
		log.Printf("IS→RF 送信失敗: %v", err)
		return
	}
	a.smu.Lock()
	a.stats.IStoRF++
	a.smu.Unlock()
	a.emitPacket("GATE-RF", line)
}

// rfGateAllow は IS→RF のレート制限 (件/分) を確認し、枠を 1 つ使う。
func (a *App) rfGateAllow(cfg Config) bool {
	a.hmu.Lock()
	defer a.hmu.Unlock()
	now := time.Now()
	kept := a.rfGateLog[:0]
	for _, t := range a.rfGateLog {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	a.rfGateLog = kept
	if len(kept) >= cfg.MaxIStoRFPerMin {
		return false
	}
	a.rfGateLog = append(a.rfGateLog, now)
	return true
}
