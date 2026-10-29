package main

// SDR で受信した気象センサー (rtl_433 の JSON 出力) の取り込み。
// rtl_433 を子プロセスとして起動し、標準出力の JSON を 1 行ずつ読んで、気象データ (Weather) に換算する。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultSdrCmd = "rtl_433 -f 433.92M -F json"
	luxPerWm2     = 126.7 // 日光での目安 (APRS の L は W/m²)
)

// SdrDevice は受信したセンサー 1 台分の最新値。
type SdrDevice struct {
	Key     string   `json:"key"` // model/id[/ch]
	Model   string   `json:"model"`
	ID      string   `json:"id"`
	Channel string   `json:"channel,omitempty"`
	Last    int64    `json:"last"` // ミリ秒
	Count   int      `json:"count"`
	WX      *Weather `json:"wx"`
	Raw     string   `json:"raw"`
}

type SdrStatus struct {
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

// rainSample は累積雨量の記録 (rtl_433 の rain_mm は累積値)。
type rainSample struct {
	t  time.Time
	mm float64
}

type rainTracker struct {
	s []rainSample
}

// add は累積値を記録し、1 時間・24 時間・0 時からの雨量を返す。累積値が減ったらリセットとみなして作り直す。
func (r *rainTracker) add(now time.Time, mm float64) (h1, h24, mid float64) {
	if n := len(r.s); n > 0 && mm < r.s[n-1].mm {
		r.s = nil
	}
	r.s = append(r.s, rainSample{now, mm})
	for len(r.s) > 1 && now.Sub(r.s[1].t) >= 24*time.Hour {
		r.s = r.s[1:]
	}
	since := func(from time.Time) float64 {
		base := r.s[0].mm
		for _, x := range r.s {
			if x.t.After(from) {
				break
			}
			base = x.mm
		}
		return math.Max(0, mm-base)
	}
	y, m, d := now.Date()
	return since(now.Add(-time.Hour)), since(now.Add(-24 * time.Hour)), since(time.Date(y, m, d, 0, 0, 0, 0, now.Location()))
}

func jnum(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f, ok := v.(float64); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func jstr(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

// parseRtl433 は rtl_433 の JSON 1 行を、センサーの識別子と気象データに換算する。
// 気象の値を 1 つも含まない行 (リモコンやタイヤ圧センサーなど) は ok=false。rain は累積値のまま返す。
func parseRtl433(line string) (dev SdrDevice, rainMm *float64, ok bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return dev, nil, false
	}
	model := jstr(m, "model")
	if model == "" {
		return dev, nil, false
	}
	w := &Weather{}
	found := false
	if v, ok := jnum(m, "temperature_C"); ok {
		w.Temp, found = fptr(v), true
	} else if v, ok := jnum(m, "temperature_F"); ok {
		w.Temp, found = fptr((v-32)/1.8), true
	}
	if v, ok := jnum(m, "humidity"); ok && v >= 0 && v <= 100 {
		w.Humidity, found = iptr(int(math.Round(v))), true
	}
	if v, ok := jnum(m, "wind_dir_deg"); ok {
		w.WindDir, found = iptr(int(math.Round(v))%360), true
	}
	if v, ok := jnum(m, "wind_avg_m_s", "wind_speed_m_s"); ok {
		w.Wind, found = fptr(v), true
	} else if v, ok := jnum(m, "wind_avg_km_h", "wind_speed_km_h"); ok {
		w.Wind, found = fptr(v/3.6), true
	}
	if v, ok := jnum(m, "wind_max_m_s", "gust_speed_m_s"); ok {
		w.Gust, found = fptr(v), true
	} else if v, ok := jnum(m, "wind_max_km_h", "gust_speed_km_h"); ok {
		w.Gust, found = fptr(v/3.6), true
	}
	if v, ok := jnum(m, "pressure_hPa"); ok {
		w.Pressure, found = fptr(v), true
	} else if v, ok := jnum(m, "pressure_kPa"); ok {
		w.Pressure, found = fptr(v*10), true
	}
	if v, ok := jnum(m, "light_lux"); ok {
		w.Luminosity, found = iptr(int(math.Round(v/luxPerWm2))), true
	}
	if v, ok := jnum(m, "rain_mm"); ok {
		rainMm, found = fptr(v), true
	}
	if !found {
		return dev, nil, false
	}
	dev = SdrDevice{Model: model, ID: jstr(m, "id"), Channel: jstr(m, "channel"), WX: w, Raw: line}
	dev.Key = model + "/" + dev.ID
	if dev.Channel != "" {
		dev.Key += "/" + dev.Channel
	}
	return dev, rainMm, true
}

// SdrWx は rtl_433 を起動して受信したセンサーを覚える。
type SdrWx struct {
	app *App

	mu      sync.Mutex
	devs    map[string]*SdrDevice
	rain    map[string]*rainTracker
	running bool
	err     string
}

func NewSdrWx(a *App) *SdrWx {
	return &SdrWx{app: a, devs: map[string]*SdrDevice{}, rain: map[string]*rainTracker{}}
}

func (s *SdrWx) Status(enabled bool) SdrStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SdrStatus{Enabled: enabled, Running: s.running, Error: s.err}
}

func (s *SdrWx) Devices() []SdrDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SdrDevice, 0, len(s.devs))
	for _, d := range s.devs {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

func (s *SdrWx) setState(running bool, err string) {
	s.mu.Lock()
	s.running, s.err = running, err
	s.mu.Unlock()
}

// Run は rtl_433 を起動して出力を読み続ける。終了したら少し待って起動し直す (ctx が終わるまで)。
func (s *SdrWx) Run(ctx context.Context) {
	cfg := s.app.Config()
	fields := strings.Fields(cfg.SdrWxCmd)
	if len(fields) == 0 {
		fields = strings.Fields(defaultSdrCmd)
	}
	wait := 5 * time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := s.runOnce(ctx, fields)
		s.setState(false, errText(err))
		if ctx.Err() != nil {
			return
		}
		log.Printf("SDR 気象: %s が終了しました (%v)", fields[0], err)
		if time.Since(started) > time.Minute {
			wait = 5 * time.Second
		} else if wait < time.Minute {
			wait *= 2
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func errText(err error) string {
	if err == nil {
		return "終了しました"
	}
	return err.Error()
}

func (s *SdrWx) runOnce(ctx context.Context, fields []string) error {
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s を起動できません: %w", fields[0], err)
	}
	s.setState(true, "")
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		s.handleLine(sc.Text(), time.Now())
	}
	return cmd.Wait()
}

// handleLine は rtl_433 の 1 行を処理する。
func (s *SdrWx) handleLine(line string, now time.Time) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return
	}
	dev, rainMm, ok := parseRtl433(line)
	if !ok {
		return
	}
	s.mu.Lock()
	if rainMm != nil {
		rt := s.rain[dev.Key]
		if rt == nil {
			rt = &rainTracker{}
			s.rain[dev.Key] = rt
		}
		h1, h24, mid := rt.add(now, *rainMm)
		dev.WX.Rain1h, dev.WX.Rain24h, dev.WX.RainMidnight = fptr(h1), fptr(h24), fptr(mid)
	}
	cur := s.devs[dev.Key]
	if cur == nil {
		if len(s.devs) >= 200 {
			s.mu.Unlock()
			return
		}
		cur = &SdrDevice{}
		s.devs[dev.Key] = cur
	}
	count := cur.Count + 1
	// 同じセンサーの別のパケット (温湿度と風が別々に届く機種) の値を引き継ぐ
	merged := mergeWeather(cur.WX, dev.WX)
	*cur = dev
	cur.WX, cur.Count, cur.Last = merged, count, now.UnixMilli()
	snap := *cur
	s.mu.Unlock()
	s.app.sdrUpdated(snap)
}

func mergeWeather(old, n *Weather) *Weather {
	if old == nil {
		return n
	}
	m := *old
	if n.WindDir != nil {
		m.WindDir = n.WindDir
	}
	if n.Wind != nil {
		m.Wind = n.Wind
	}
	if n.Gust != nil {
		m.Gust = n.Gust
	}
	if n.Temp != nil {
		m.Temp = n.Temp
	}
	if n.Rain1h != nil {
		m.Rain1h, m.Rain24h, m.RainMidnight = n.Rain1h, n.Rain24h, n.RainMidnight
	}
	if n.Humidity != nil {
		m.Humidity = n.Humidity
	}
	if n.Pressure != nil {
		m.Pressure = n.Pressure
	}
	if n.Luminosity != nil {
		m.Luminosity = n.Luminosity
	}
	return &m
}

// sdrWxCall は SDR の気象を表示する局のコール。未設定のときは自局コールに -13 を付ける。
func sdrWxCall(cfg Config) string {
	if c := strings.ToUpper(strings.TrimSpace(cfg.SdrWxCall)); c != "" {
		return c
	}
	if cfg.Callsign == "" {
		return ""
	}
	base := strings.ToUpper(cfg.Callsign)
	if i := strings.IndexByte(base, '-'); i >= 0 {
		base = base[:i]
	}
	return base + "-13"
}

// sdrUpdated は受信したセンサーが選択中のものなら、気象局として地図に反映する。
func (a *App) sdrUpdated(d SdrDevice) {
	cfg := a.Config()
	a.hub.Broadcast("sdrwx", d)
	if !cfg.SdrWxEnabled || cfg.SdrWxDevice == "" || cfg.SdrWxDevice != d.Key {
		return
	}
	call := sdrWxCall(cfg)
	if call == "" {
		return
	}
	a.updateStation(&Packet{
		Raw: d.Raw, Src: call, Dst: toCall, Type: "weather", HasPos: true, Lat: cfg.Lat, Lon: cfg.Lon,
		SymTable: '/', SymCode: '_', WX: d.WX, Comment: "SDR " + d.Model,
	}, "LOCAL")
}
