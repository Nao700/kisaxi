package main

// 自局の気象ステーション: 読み取った値の保持 (現在値・履歴・雨量の積算) と、APRS WX パケットの送信。

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	wxBucket      = 5 * time.Minute    // 履歴の集計単位
	wxHistKeep    = 7 * 24 * time.Hour // 履歴の保持期間
	wxGustWindow  = 5 * time.Minute    // 突風 (g) は過去 5 分間の最大風速
	wxStale       = 5 * time.Minute    // これより古い値は送信しない
	wxSaveEvery   = 10 * time.Minute   // 履歴ファイルの保存間隔
	wxRainResume  = 60 * time.Minute   // 再起動をまたいで雨量の増分を数える最大の間隔
	wxMapUpdateIn = 60 * time.Second   // 地図上の自局 WX 局の更新間隔
	wxRainEpsMm   = 0.0005
)

// WXPoint は 5 分ごとの集計値。欠測の項目は省く。
type WXPoint struct {
	T       int64    `json:"t"` // 区間の開始 (ミリ秒)
	Temp    *float64 `json:"temp,omitempty"`
	TempMin *float64 `json:"tempMin,omitempty"`
	TempMax *float64 `json:"tempMax,omitempty"`
	Hum     *float64 `json:"hum,omitempty"`
	Pres    *float64 `json:"pres,omitempty"`
	Wind    *float64 `json:"wind,omitempty"`
	Gust    *float64 `json:"gust,omitempty"`
	Solar   *float64 `json:"solar,omitempty"`
	Rain    float64  `json:"rain"` // 区間内の雨量 (mm)
}

type wxRainEvent struct {
	T  int64   `json:"t"`
	Mm float64 `json:"mm"`
}

type wxWindSample struct {
	t time.Time
	v float64
}

// wxAvg は平均値の積算用。
type wxAvg struct {
	sum float64
	n   int
}

func (a *wxAvg) add(v float64) { a.sum += v; a.n++ }
func (a *wxAvg) ptr() *float64 {
	if a.n == 0 {
		return nil
	}
	return fptr(a.sum / float64(a.n))
}

type wxAccum struct {
	start                      time.Time
	temp, hum, pres, wind, sol wxAvg
	tMin, tMax                 *float64
	gust                       *float64
	rain                       float64
}

func (c *wxAccum) point() WXPoint {
	return WXPoint{T: c.start.UnixMilli(), Temp: c.temp.ptr(), TempMin: c.tMin, TempMax: c.tMax,
		Hum: c.hum.ptr(), Pres: c.pres.ptr(), Wind: c.wind.ptr(), Gust: c.gust, Solar: c.sol.ptr(), Rain: c.rain}
}

func (c *wxAccum) empty() bool {
	return c.temp.n+c.hum.n+c.pres.n+c.wind.n+c.sol.n == 0 && c.rain == 0
}

type wxState struct {
	mu sync.Mutex

	cur  *Weather // 最新の値 (突風と 1 時間・24 時間雨量を補ったもの)
	curT time.Time

	acc   *wxAccum
	hist  []WXPoint
	rain  []wxRainEvent // 過去 24 時間の雨量の増分
	winds []wxWindSample

	baseMm  float64 // 前回読んだ日雨量 (mm)
	baseT   time.Time
	baseSet bool

	dirty    bool
	lastSave time.Time
	nextRF   time.Time
	nextIS   time.Time
	nextCWOP time.Time
	cwopBusy bool
	lastCWOP string // 直近の CWOP 送信結果 (表示用)
	lastPkt  string
	lastTx   int64
	nextStn  time.Time // 地図上の局を次に更新する時刻
}

// wxCallsign は気象局として送信するコールサイン-SSID。未指定なら自局のコールに -13 (自局が -13 なら -14)。
func wxCallsign(cfg Config) string {
	if cfg.WXCall != "" {
		return cfg.WXCall
	}
	if cfg.Callsign == "" {
		return ""
	}
	base, ssid := cfg.Callsign, "13"
	if i := strings.IndexByte(base, '-'); i >= 0 {
		base = base[:i]
	}
	if sameCall(cfg.Callsign, base+"-13") {
		ssid = "14"
	}
	return base + "-" + ssid
}

// isOwnCall は自局または気象局のコールサインか (自分の送信が戻ってきたものを無視するための判定)。
func isOwnCall(call string, cfg Config) bool {
	if cfg.Callsign == "" {
		return false
	}
	if sameCall(call, cfg.Callsign) {
		return true
	}
	return cfg.WXEnabled && sameCall(call, wxCallsign(cfg))
}

func f64(v float64) *float64 { return &v }

// wxIngest は Davis から読んだ値を取り込む。
func (a *App) wxIngest(w *Weather, now time.Time) {
	st := &a.wx
	st.mu.Lock()

	// 雨量: 日雨量の増分を積算する。0 時のリセット (値が減る) は新しい日雨量をそのまま増分とみなす
	delta := 0.0
	if w.RainMidnight != nil {
		day := *w.RainMidnight
		if st.baseSet && now.Sub(st.baseT) <= wxRainResume {
			if day+wxRainEpsMm >= st.baseMm {
				delta = day - st.baseMm
			} else {
				delta = day
			}
		}
		if delta < wxRainEpsMm {
			delta = 0
		}
		st.baseMm, st.baseT, st.baseSet = day, now, true
	}
	if delta > 0 {
		st.rain = append(st.rain, wxRainEvent{T: now.UnixMilli(), Mm: delta})
		st.dirty = true
	}
	limit := now.Add(-24 * time.Hour).UnixMilli()
	for len(st.rain) > 0 && st.rain[0].T < limit {
		st.rain = st.rain[1:]
	}
	r1, r24 := 0.0, 0.0
	h1 := now.Add(-time.Hour).UnixMilli()
	for _, e := range st.rain {
		r24 += e.Mm
		if e.T >= h1 {
			r1 += e.Mm
		}
	}

	// 突風: 過去 5 分間の最大風速
	if w.Wind != nil {
		st.winds = append(st.winds, wxWindSample{now, *w.Wind})
	}
	for len(st.winds) > 0 && now.Sub(st.winds[0].t) > wxGustWindow {
		st.winds = st.winds[1:]
	}
	if len(st.winds) > 0 {
		g := st.winds[0].v
		for _, s := range st.winds {
			if s.v > g {
				g = s.v
			}
		}
		w.Gust = fptr(g)
	}
	if w.RainMidnight != nil {
		w.Rain1h, w.Rain24h = fptr(r1), fptr(r24)
	}

	// 5 分ごとの集計
	slot := now.Truncate(wxBucket)
	if st.acc != nil && !st.acc.start.Equal(slot) {
		st.closeBucket(now)
	}
	if st.acc == nil {
		st.acc = &wxAccum{start: slot}
	}
	c := st.acc
	if w.Temp != nil {
		c.temp.add(*w.Temp)
		if c.tMin == nil || *w.Temp < *c.tMin {
			c.tMin = f64(*w.Temp)
		}
		if c.tMax == nil || *w.Temp > *c.tMax {
			c.tMax = f64(*w.Temp)
		}
	}
	if w.Humidity != nil {
		c.hum.add(float64(*w.Humidity))
	}
	if w.Pressure != nil {
		c.pres.add(*w.Pressure)
	}
	if w.Wind != nil {
		c.wind.add(*w.Wind)
	}
	if w.Luminosity != nil {
		c.sol.add(float64(*w.Luminosity))
	}
	if w.Wind != nil && (c.gust == nil || *w.Wind > *c.gust) {
		c.gust = f64(*w.Wind)
	}
	c.rain += delta

	st.cur, st.curT = w, now
	cur := *w
	st.mu.Unlock()

	a.hub.Broadcast("wx", map[string]any{"t": now.UnixMilli(), "cur": cur})
}

// closeBucket は集計中の区間を履歴に確定する (st.mu を取得済みで呼ぶ)。
func (st *wxState) closeBucket(now time.Time) {
	if st.acc != nil && !st.acc.empty() {
		st.hist = append(st.hist, st.acc.point())
		st.dirty = true
	}
	st.acc = nil
	limit := now.Add(-wxHistKeep).UnixMilli()
	i := 0
	for i < len(st.hist) && st.hist[i].T < limit {
		i++
	}
	st.hist = st.hist[i:]
}

// WXSnapshot は画面に渡す現在値・履歴・送信状況。
type WXSnapshot struct {
	Enabled  bool         `json:"enabled"`
	Call     string       `json:"call"`
	Cur      *Weather     `json:"cur,omitempty"`
	CurT     int64        `json:"curT,omitempty"`
	History  []WXPoint    `json:"history"`
	Davis    *DavisStatus `json:"davis,omitempty"`
	LastPkt  string       `json:"lastPkt,omitempty"`
	LastTx   int64        `json:"lastTx,omitempty"`
	NextIS   int64        `json:"nextIs,omitempty"`
	NextRF   int64        `json:"nextRf,omitempty"`
	CWOP     string       `json:"cwop,omitempty"` // 直近の CWOP 送信結果
	NextCWOP int64        `json:"nextCwop,omitempty"`
}

func (a *App) WX() WXSnapshot {
	cfg := a.Config()
	st := &a.wx
	st.mu.Lock()
	defer st.mu.Unlock()
	s := WXSnapshot{Enabled: cfg.WXEnabled, Call: wxCallsign(cfg), History: append([]WXPoint{}, st.hist...),
		LastPkt: st.lastPkt, LastTx: st.lastTx}
	if st.acc != nil && !st.acc.empty() {
		s.History = append(s.History, st.acc.point())
	}
	if st.cur != nil {
		c := *st.cur
		s.Cur, s.CurT = &c, st.curT.UnixMilli()
	}
	s.Davis = a.davisStatus()
	if cfg.WXEnabled && cfg.Callsign != "" {
		if cfg.WXIS && !st.nextIS.IsZero() {
			s.NextIS = st.nextIS.UnixMilli()
		}
		if cfg.WXCWOP {
			s.CWOP = st.lastCWOP
			if !st.nextCWOP.IsZero() {
				s.NextCWOP = st.nextCWOP.UnixMilli()
			}
		}
		if cfg.WXRF && !st.nextRF.IsZero() {
			s.NextRF = st.nextRF.UnixMilli()
		}
	}
	return s
}

func (a *App) davisStatus() *DavisStatus {
	a.mu.RLock()
	d := a.davis
	a.mu.RUnlock()
	if d == nil {
		return nil
	}
	s := d.Status()
	return &s
}

// wxPacket は現在値から送信するパケット本文を作る。値が古い・無いときは ok=false。
func (a *App) wxPacket(cfg Config, now time.Time) (body string, ok bool) {
	st := &a.wx
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cur == nil || now.Sub(st.curT) > wxStale {
		return "", false
	}
	return wxPacketBody(cfg.Lat, cfg.Lon, st.cur, now), true
}

// sendWX は気象パケットを送り、送れた経路を返す。
func (a *App) sendWX(cfg Config, now time.Time, rf, isNet bool) (sentRF, sentIS bool, err error) {
	call := wxCallsign(cfg)
	if call == "" {
		return false, false, errors.New("コールサインが未設定です")
	}
	body, ok := a.wxPacket(cfg, now)
	if !ok {
		return false, false, errors.New("気象データを受信していません")
	}
	rfLine := call + ">" + toCall + pathSuffix(cfg.BeaconRFPath) + ":" + body
	isLine := call + ">" + toCall + ",TCPIP*:" + body
	sentRF, sentIS = a.transmitLines(rfLine, isLine, rf, isNet)
	if sentRF || sentIS {
		st := &a.wx
		st.mu.Lock()
		st.lastPkt, st.lastTx = isLine, now.UnixMilli()
		st.mu.Unlock()
		a.updateWXStation(isLine)
	}
	return
}

// updateWXStation は送信した気象パケットを地図上の自局の気象局として反映する。
func (a *App) updateWXStation(line string) {
	if p, err := ParseTNC2(line); err == nil {
		a.updateStation(p, "LOCAL")
	}
}

// tickWX は定期処理: 区間の確定、地図上の局の更新、送信間隔が来たら送信。
func (a *App) tickWX(now time.Time) {
	cfg := a.Config()
	st := &a.wx
	st.mu.Lock()
	if st.acc != nil && now.Truncate(wxBucket).After(st.acc.start) {
		st.closeBucket(now)
	}
	if st.dirty && now.Sub(st.lastSave) >= wxSaveEvery {
		st.mu.Unlock()
		a.saveWX()
		st.mu.Lock()
	}
	st.mu.Unlock()

	if !cfg.WXEnabled || cfg.Callsign == "" {
		return
	}
	st.mu.Lock()
	doRF := cfg.WXRF && !now.Before(st.nextRF)
	doIS := cfg.WXIS && !now.Before(st.nextIS)
	updStn := !now.Before(st.nextStn)
	st.mu.Unlock()

	a.tickCWOP(cfg, now)

	sent := false
	if doRF || doIS {
		sentRF, sentIS, err := a.sendWX(cfg, now, doRF, doIS)
		if err == nil {
			sent = sentRF || sentIS
			st.mu.Lock()
			if sentRF {
				st.nextRF = now.Add(time.Duration(cfg.WXRFMin) * time.Minute)
			}
			if sentIS {
				st.nextIS = now.Add(time.Duration(cfg.WXISMin) * time.Minute)
			}
			st.mu.Unlock()
		}
	}
	// 送れなかった (APRS-IS・TNC 未接続など) ときも、地図上の自局の気象局は定期的に更新する
	if updStn || sent {
		if !sent {
			if body, ok := a.wxPacket(cfg, now); ok {
				a.updateWXStation(wxCallsign(cfg) + ">" + toCall + ",TCPIP*:" + body)
			}
		}
		st.mu.Lock()
		st.nextStn = now.Add(wxMapUpdateIn)
		st.mu.Unlock()
	}
}

// WXSendNow は「今すぐ送信」。設定で有効な経路 (APRS-IS / RF) に送る。
func (a *App) WXSendNow() error {
	cfg := a.Config()
	if !cfg.WXEnabled {
		return errors.New("ウェザーステーションが有効になっていません")
	}
	if !cfg.WXIS && !cfg.WXRF && !cfg.WXCWOP {
		return errors.New("設定で APRS-IS・RF・CWOP のいずれかの送信を有効にしてください")
	}
	now := time.Now()
	if cfg.WXCWOP {
		a.sendCWOPNow(cfg, now)
		if !cfg.WXIS && !cfg.WXRF {
			return nil
		}
	}
	sentRF, sentIS, err := a.sendWX(cfg, now, cfg.WXRF, cfg.WXIS)
	if err != nil {
		return err
	}
	if !sentRF && !sentIS {
		return errors.New("送信できませんでした (APRS-IS / TNC が未接続です)")
	}
	st := &a.wx
	st.mu.Lock()
	if sentRF {
		st.nextRF = now.Add(time.Duration(cfg.WXRFMin) * time.Minute)
	}
	if sentIS {
		st.nextIS = now.Add(time.Duration(cfg.WXISMin) * time.Minute)
	}
	st.mu.Unlock()
	return nil
}

// ---------- 保存と復元 ----------

type savedWX struct {
	Saved  int64         `json:"saved"`
	Hist   []WXPoint     `json:"hist"`
	Rain   []wxRainEvent `json:"rain"`
	BaseMm float64       `json:"baseMm"`
	BaseT  int64         `json:"baseT"`
}

func (a *App) wxPath() string {
	return legacyName(filepath.Dir(a.cfgPath), "kisaxi-wx.json", "aprsigate-wx.json")
}

// saveWX は気象の履歴をファイルに保存する (集計中の区間も含める)。
func (a *App) saveWX() {
	st := &a.wx
	st.mu.Lock()
	d := savedWX{Saved: time.Now().UnixMilli(), Hist: append([]WXPoint{}, st.hist...), Rain: append([]wxRainEvent{}, st.rain...)}
	if st.acc != nil && !st.acc.empty() {
		d.Hist = append(d.Hist, st.acc.point())
	}
	if st.baseSet {
		d.BaseMm, d.BaseT = st.baseMm, st.baseT.UnixMilli()
	}
	st.dirty = false
	st.lastSave = time.Now()
	st.mu.Unlock()

	b, err := json.Marshal(d)
	if err != nil {
		log.Printf("気象データの保存に失敗しました: %v", err)
		return
	}
	path := a.wxPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Printf("気象データの保存に失敗しました: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("気象データの保存に失敗しました: %v", err)
	}
}

// SaveWX は終了時など、間隔にかかわらず気象の履歴を保存する。
func (a *App) SaveWX() {
	a.wx.mu.Lock()
	has := len(a.wx.hist) > 0 || a.wx.acc != nil || len(a.wx.rain) > 0
	a.wx.mu.Unlock()
	if has {
		a.saveWX()
	}
}

// LoadWX は保存された気象の履歴を復元する。
func (a *App) LoadWX() {
	b, err := os.ReadFile(a.wxPath())
	if err != nil {
		return
	}
	var d savedWX
	if err := json.Unmarshal(b, &d); err != nil {
		log.Printf("気象データの読み込みに失敗しました: %v", err)
		return
	}
	now := time.Now()
	st := &a.wx
	st.mu.Lock()
	defer st.mu.Unlock()
	limit := now.Add(-wxHistKeep).UnixMilli()
	for _, p := range d.Hist {
		if p.T >= limit {
			st.hist = append(st.hist, p)
		}
	}
	h24 := now.Add(-24 * time.Hour).UnixMilli()
	for _, e := range d.Rain {
		if e.T >= h24 {
			st.rain = append(st.rain, e)
		}
	}
	if d.BaseT > 0 {
		st.baseMm, st.baseT, st.baseSet = d.BaseMm, time.UnixMilli(d.BaseT), true
	}
}

// tickCWOP は CWOP への送信間隔が来ていれば、別 goroutine で送信する (通信で定期処理を止めないため)。
func (a *App) tickCWOP(cfg Config, now time.Time) {
	if !cfg.WXCWOP {
		return
	}
	st := &a.wx
	st.mu.Lock()
	due := !st.cwopBusy && !now.Before(st.nextCWOP)
	st.mu.Unlock()
	if due {
		a.sendCWOPNow(cfg, now)
	}
}

// sendCWOPNow は気象パケットを CWOP へ送る。データが無いときは何もしない。
func (a *App) sendCWOPNow(cfg Config, now time.Time) {
	body, ok := a.wxPacket(cfg, now)
	if !ok {
		return
	}
	id := cwopID(cfg)
	line := cwopLine(id, body)
	st := &a.wx
	st.mu.Lock()
	st.cwopBusy = true
	st.nextCWOP = now.Add(time.Duration(cfg.WXCWOPMin) * time.Minute)
	st.mu.Unlock()
	go func() {
		err := sendCWOP(cfg, id, line)
		res := "CWOP 送信 " + time.Now().Format("15:04") + " 成功"
		if err != nil {
			res = "CWOP 送信失敗: " + err.Error()
			a.emitPacket("TX", "# "+res)
		} else {
			a.emitPacket("TX-CWOP", line)
		}
		st.mu.Lock()
		st.cwopBusy = false
		st.lastCWOP = res
		st.mu.Unlock()
	}()
}
