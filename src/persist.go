package main

// 受信データ (局・パケットログ) の保存と復元。アプリを再起動しても保持時間内のデータを引き継ぐ。

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

type savedData struct {
	Saved    int64         `json:"saved"`
	Stations []*Station    `json:"stations"`
	Packets  []PacketEvent `json:"packets"`
	DX       *DXRecord     `json:"dx,omitempty"`
}

func (a *App) dataPath() string {
	return filepath.Join(filepath.Dir(a.cfgPath), "aprsigate-data.json")
}

func (a *App) retention() time.Duration {
	return time.Duration(a.Config().StationExpireMin) * time.Minute
}

// SaveData は現在の受信データをファイルに保存する。
func (a *App) SaveData() {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	d := savedData{Saved: time.Now().UnixMilli(), Stations: a.db.All(), Packets: a.recent.All(), DX: a.DX()}
	b, err := json.Marshal(d)
	if err != nil {
		log.Printf("受信データの保存に失敗しました: %v", err)
		return
	}
	path := a.dataPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Printf("受信データの保存に失敗しました: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("受信データの保存に失敗しました: %v", err)
	}
}

// LoadData は保存された受信データのうち、保持時間内のものを復元する。
func (a *App) LoadData() {
	b, err := os.ReadFile(a.dataPath())
	if err != nil {
		return
	}
	var d savedData
	if err := json.Unmarshal(b, &d); err != nil {
		log.Printf("受信データの読み込みに失敗しました: %v", err)
		return
	}
	now := time.Now()
	keep := a.retention()
	n := a.db.Load(d.Stations, keep, now)
	limit := now.Add(-keep).UnixMilli()
	np := 0
	for _, p := range d.Packets {
		if p.T >= limit {
			a.recent.Add(p)
			np++
		}
	}
	if d.DX != nil && d.DX.Period == dxPeriod(now, a.Config().DXReset) {
		a.dxMu.Lock()
		a.dx = d.DX // 同じ期間内の記録だけ引き継ぐ (送信済みとみなす)
		a.dxMu.Unlock()
	}
	if n > 0 || np > 0 {
		log.Printf("前回の受信データを復元しました (局 %d / パケット %d)", n, np)
	}
}
