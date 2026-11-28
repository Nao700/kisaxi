package main

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type TrackPoint [3]float64 // lat, lon, unix 秒

type Station struct {
	Call    string       `json:"call"`
	HasPos  bool         `json:"hasPos"`
	Lat     float64      `json:"lat"`
	Lon     float64      `json:"lon"`
	Ambig   int          `json:"ambig,omitempty"` // 位置曖昧さレベル 1〜4
	Sym     string       `json:"sym"`
	Comment string       `json:"comment"`
	Status  string       `json:"status"`
	HasCS   bool         `json:"hasCs"`
	Course  int          `json:"course"`
	Speed   float64      `json:"speed"`
	HasAlt  bool         `json:"hasAlt"`
	Alt     float64      `json:"alt"`
	WX      *Weather     `json:"wx,omitempty"`
	WXTime  int64        `json:"wxTime,omitempty"` // 気象データの受信時刻 (ミリ秒)
	MicE    string       `json:"micE"`
	Via     string       `json:"via"` // RF / IS / LOCAL
	Path    string       `json:"path"`
	Raw     string       `json:"raw"`
	Type    string       `json:"type"`
	Object  bool         `json:"object"`
	Owner   string       `json:"owner"`
	Killed  bool         `json:"killed"`
	Last    int64        `json:"last"`   // ミリ秒
	LastRF  int64        `json:"lastRf"` // ミリ秒
	Count   int          `json:"count"`
	Track   []TrackPoint `json:"track,omitempty"`
	// TrackPath は Track と同じ並びで、各ポイントを RF で受けたときの経路 ("RF" または "RF,JA1DIG*,WIDE2-1")。
	// IS 受信や旧データは空文字。旧データでは Track より短いことがあり、その場合は末尾に揃える。
	TrackPath []string `json:"trackPath,omitempty"`

	// 位置の外れ値検査用 (保存しない)。急に遠くへ飛んだ位置を 1 回だけ保留する。
	pendLat, pendLon float64
	hasPend          bool
}

func (s *Station) clone() *Station {
	c := *s
	c.Track = append([]TrackPoint(nil), s.Track...)
	c.TrackPath = append([]string(nil), s.TrackPath...)
	return &c
}

type StationDB struct {
	mu sync.Mutex
	m  map[string]*Station
}

func NewStationDB() *StationDB { return &StationDB{m: map[string]*Station{}} }

func (db *StationDB) Update(p *Packet, via string, now time.Time, maxTrack int) *Station {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := p.Key()
	s := db.m[key]
	if s == nil {
		s = &Station{Call: key}
		db.m[key] = s
	}
	ms := now.UnixMilli()
	prevLast := s.Last
	s.Last = ms
	s.Count++
	s.Via = via
	if via == "RF" {
		s.LastRF = ms
	}
	s.Path = strings.Join(p.Path, ",")
	s.Raw = displayText(p.Raw)
	if p.Type != "other" {
		s.Type = p.Type
	}
	if p.Name != "" {
		s.Object = true
		s.Owner = p.Src
		s.Killed = p.Killed
	}
	if p.HasPos && s.HasPos && p.Name == "" && implausibleJump(s, p, now, prevLast) {
		// 受信ミス (化けたパケット) の疑い。位置は採用せず、次のパケットが同じ場所なら本当の移動として採用する。
		if s.hasPend && distanceKm(s.pendLat, s.pendLon, p.Lat, p.Lon) <= 5 {
			s.hasPend = false
		} else {
			s.pendLat, s.pendLon, s.hasPend = p.Lat, p.Lon, true
			return s.clone()
		}
	} else if p.HasPos {
		s.hasPend = false
	}
	if p.HasPos {
		if s.HasPos && maxTrack > 0 && distanceKm(s.Lat, s.Lon, p.Lat, p.Lon) > 0.02 {
			// 旧データ (経路なし) は先頭を空文字で埋めて Track と長さを揃える
			for len(s.TrackPath) < len(s.Track) {
				s.TrackPath = append([]string{""}, s.TrackPath...)
			}
			if len(s.Track) == 0 {
				s.Track = append(s.Track, TrackPoint{s.Lat, s.Lon, float64(prevLast / 1000)})
				s.TrackPath = append(s.TrackPath, "")
			}
			route := ""
			if via == "RF" {
				route = strings.Join(append([]string{"RF"}, p.Path...), ",")
			}
			s.Track = append(s.Track, TrackPoint{p.Lat, p.Lon, float64(now.Unix())})
			s.TrackPath = append(s.TrackPath, route)
			if len(s.Track) > maxTrack {
				s.Track = s.Track[len(s.Track)-maxTrack:]
				s.TrackPath = s.TrackPath[len(s.TrackPath)-maxTrack:]
			}
		}
		s.HasPos = true
		s.Lat, s.Lon = p.Lat, p.Lon
		s.Ambig = p.Ambig
		if sym := p.Symbol(); sym != "" {
			s.Sym = sym
		}
		s.Comment = displayText(p.Comment)
		s.HasCS, s.Course, s.Speed = p.HasCS, p.Course, p.Speed
		if p.HasAlt {
			s.HasAlt, s.Alt = true, p.Alt
		}
		s.MicE = p.MicEMsg
	}
	if p.Status != "" {
		s.Status = displayText(p.Status)
	}
	if p.WX != nil {
		s.WX, s.WXTime = p.WX, ms
		if !p.HasPos && p.Comment != "" {
			s.Comment = displayText(p.Comment)
		}
	}
	return s.clone()
}

// Load は保存データから局を復元する (maxAge より古いものは捨てる)。
func (db *StationDB) Load(list []*Station, maxAge time.Duration, now time.Time) int {
	db.mu.Lock()
	defer db.mu.Unlock()
	limit := now.Add(-maxAge).UnixMilli()
	n := 0
	for _, s := range list {
		if s == nil || s.Call == "" || s.Last < limit {
			continue
		}
		if cur, ok := db.m[s.Call]; ok && cur.Last >= s.Last {
			continue
		}
		s.Track, s.TrackPath = dropTrackSpikes(s.Track, s.TrackPath)
		db.m[s.Call] = s
		n++
	}
	return n
}

// Expire は指定時間より古い局を削除し、そのキーを返す。
func (db *StationDB) Expire(maxAge time.Duration, now time.Time) []string {
	db.mu.Lock()
	defer db.mu.Unlock()
	limit := now.Add(-maxAge).UnixMilli()
	var removed []string
	for k, s := range db.m {
		if s.Last < limit {
			delete(db.m, k)
			removed = append(removed, k)
		}
	}
	return removed
}

func (db *StationDB) All() []*Station {
	db.mu.Lock()
	defer db.mu.Unlock()
	out := make([]*Station, 0, len(db.m))
	for _, s := range db.m {
		out = append(out, s.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

func distanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dlat := (lat2 - lat1) * rad
	dlon := (lon2 - lon1) * rad
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}

// implausibleJump は、前回の位置から現実的でない速さ・距離で飛んだ位置かを返す。
// 300km 以上離れ、かつ平均 1000km/h を超える移動を外れ値とみなす (航空機は超えない想定)。
func implausibleJump(s *Station, p *Packet, now time.Time, prevLastMs int64) bool {
	d := distanceKm(s.Lat, s.Lon, p.Lat, p.Lon)
	if d < 300 {
		return false
	}
	dt := now.Sub(time.UnixMilli(prevLastMs)).Hours()
	if dt < 1.0/60 {
		dt = 1.0 / 60
	}
	return d/dt > 1000
}

// dropTrackSpikes は保存済みの軌跡から、前後の点と比べて一瞬だけ遠くへ飛んだ点 (化けたパケット由来) を取り除く。
func dropTrackSpikes(tr []TrackPoint, path []string) ([]TrackPoint, []string) {
	if len(tr) < 3 {
		return tr, path
	}
	off := len(tr) - len(path) // 旧データでは path が短い (末尾に揃える)
	outT := make([]TrackPoint, 0, len(tr))
	outP := make([]string, 0, len(path))
	for i, pt := range tr {
		if i > 0 && i < len(tr)-1 {
			prev, next := tr[i-1], tr[i+1]
			if distanceKm(prev[0], prev[1], pt[0], pt[1]) >= 300 &&
				distanceKm(pt[0], pt[1], next[0], next[1]) >= 300 &&
				distanceKm(prev[0], prev[1], next[0], next[1]) < 300 {
				continue
			}
		}
		outT = append(outT, pt)
		if j := i - off; j >= 0 && j < len(path) {
			outP = append(outP, path[j])
		}
	}
	return outT, outP
}
