package main

import "strings"

// DPRS (D-STAR の位置情報) 由来のパケットかどうかを、宛先 (tocall) で判定する。
// シンボルのオーバーレイ「D」では判定しない (D 以外の局を巻き込まない・D 付きの別の局を巻き込まないため)。
//
//	APDPRS  D-Star (DPRS) ゲートウェイ
//	APDG??  ircDDB Gateway / DStarGateway
//	APD5T?  Open Source DStarGateway
//	APJID2  D-Star ゲートウェイ
//	APnnnD  uSmartDigi D-Gate (nnn は数字)
//	API???  Icom の D-STAR 機 (ID-51 など)
func isDPRS(p *Packet) bool {
	d := strings.ToUpper(p.Dst)
	if i := strings.IndexByte(d, '-'); i >= 0 {
		d = d[:i]
	}
	switch {
	case d == "APDPRS", d == "APJID2":
		return true
	case len(d) == 6 && (strings.HasPrefix(d, "APDG") || strings.HasPrefix(d, "APD5T") || strings.HasPrefix(d, "API")):
		return true
	case len(d) == 6 && strings.HasPrefix(d, "AP") && d[5] == 'D' && isDigits(d[2:5]):
		return true
	}
	return false
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// ignoredPacket は除外局の指定、または DPRS 除外の設定により、受信を完全に無視するパケットか。
func ignoredPacket(cfg Config, p *Packet) bool {
	return noRF(cfg.NoRFCalls, p.Src) || (cfg.NoDPRS && isDPRS(p))
}
