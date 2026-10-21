package main

// 最長距離 (DX) の記録。RF で直接受信した局のうち最も遠いものを覚え、更新したらステータス ">DX: …" を送る。

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const dxSendInterval = 10 * time.Minute // DX ステータスの最短送信間隔

type DXRecord struct {
	Call    string  `json:"call"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Km      float64 `json:"km"`
	Bearing int     `json:"bearing"`
	T       int64   `json:"t"`      // 受信時刻 (ミリ秒)
	Period  string  `json:"period"` // 記録の対象期間 (リセット単位)
}

// dxPeriod はリセット単位ごとの期間キー (ローカル時刻)。
func dxPeriod(t time.Time, reset string) string {
	if reset == "hour" {
		return t.Format("2006-01-02T15")
	}
	return t.Format("2006-01-02")
}

func bearingDeg(lat1, lon1, lat2, lon2 float64) int {
	rad := math.Pi / 180
	y := math.Sin((lon2-lon1)*rad) * math.Cos(lat2*rad)
	x := math.Cos(lat1*rad)*math.Sin(lat2*rad) - math.Sin(lat1*rad)*math.Cos(lat2*rad)*math.Cos((lon2-lon1)*rad)
	b := int(math.Round(math.Atan2(y, x)/rad+360)) % 360
	return b
}

// dxCandidate は DX の対象になるパケットか (RF 直接受信・本人の位置・位置あいまいなし)。
func dxCandidate(p *Packet) bool {
	if !p.HasPos || p.Name != "" || p.Ambig > 0 || p.Type == "thirdparty" {
		return false
	}
	for _, d := range p.Path {
		if strings.HasSuffix(d, "*") {
			return false // デジピート経由
		}
	}
	return true
}

// dxStatusText はステータス本文 (先頭の ">" を除く) を作る。
// 例: DX: JJ2YKR-2 35.29.80N 137.15.28E 46.8 km 80deg 08:20
func dxStatusText(r DXRecord, unit string) string {
	lat, lon := formatLat(r.Lat), formatLon(r.Lon)
	lat = lat[:2] + "." + lat[2:]
	lon = lon[:3] + "." + lon[3:]
	return fmt.Sprintf("DX: %s %s %s %s %ddeg %s", r.Call, lat, lon, dxDistText(r.Km, unit), r.Bearing,
		time.UnixMilli(r.T).Format("15:04"))
}

func dxDistText(km float64, unit string) string {
	if unit == "mi" {
		return fmt.Sprintf("%.1f miles", km/1.609344)
	}
	return fmt.Sprintf("%.1f km", km)
}

// checkDX は RF 直接受信したパケットで DX 記録を更新する。
func (a *App) checkDX(p *Packet, cfg Config, now time.Time) {
	if !dxCandidate(p) || (cfg.Lat == 0 && cfg.Lon == 0) {
		return
	}
	km := distanceKm(cfg.Lat, cfg.Lon, p.Lat, p.Lon)
	period := dxPeriod(now, cfg.DXReset)
	a.dxMu.Lock()
	if a.dx != nil && a.dx.Period == period && km <= a.dx.Km {
		a.dxMu.Unlock()
		return
	}
	a.dx = &DXRecord{Call: p.Src, Lat: p.Lat, Lon: p.Lon, Km: km,
		Bearing: bearingDeg(cfg.Lat, cfg.Lon, p.Lat, p.Lon), T: now.UnixMilli(), Period: period}
	a.dxPending = cfg.DXEnabled
	a.dxMu.Unlock()
	a.broadcastStatus()
}

// tickDX は期間が変わった記録を消し、未送信の新記録があれば送る (送信は最短 10 分間隔)。
func (a *App) tickDX(now time.Time) {
	cfg := a.Config()
	a.dxMu.Lock()
	if a.dx != nil && a.dx.Period != dxPeriod(now, cfg.DXReset) {
		a.dx, a.dxPending = nil, false
		a.dxMu.Unlock()
		a.broadcastStatus()
		return
	}
	if !cfg.DXEnabled {
		a.dxPending = false
	}
	if !a.dxPending || now.Before(a.dxNext) || cfg.Callsign == "" {
		a.dxMu.Unlock()
		return
	}
	r := *a.dx
	a.dxMu.Unlock()

	rf, is := a.transmit(">"+dxStatusText(r, cfg.DXUnit), cfg.DXRF, true, cfg)
	if rf || is {
		a.dxMu.Lock()
		a.dxNext = now.Add(dxSendInterval)
		if a.dx != nil && a.dx.T == r.T {
			a.dxPending = false
		}
		a.dxMu.Unlock()
	}
}

// DX は現在の DX 記録を返す (無ければ nil)。
func (a *App) DX() *DXRecord {
	a.dxMu.Lock()
	defer a.dxMu.Unlock()
	if a.dx == nil {
		return nil
	}
	r := *a.dx
	return &r
}
