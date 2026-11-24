package main

// I-Gate 本体: RF⇔APRS-IS ゲート、ビーコン、局データベース、メッセージ

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

const toCall = "APKSXI" // aprs-deviceid 登録済み TOCALL (KISAXI)

type Stats struct {
	RFRx   int `json:"rfRx"`
	ISRx   int `json:"isRx"`
	RFtoIS int `json:"rfToIs"`
	IStoRF int `json:"isToRf"`
	TxRF   int `json:"txRf"`
	TxIS   int `json:"txIs"`
	Digi   int `json:"digi"`
}

type PacketEvent struct {
	T   int64  `json:"t"`
	Dir string `json:"dir"` // RF, IS, TX-RF, TX-IS, GATE-IS, GATE-RF
	Raw string `json:"raw"`
}

type App struct {
	cfgPath string
	started time.Time

	startMu sync.Mutex
	saveMu  sync.Mutex
	running sync.WaitGroup

	mu     sync.RWMutex
	cfg    Config
	is     *ISClient
	tnc    *TNC
	davis  wxClient
	cancel context.CancelFunc

	db     *StationDB
	msgs   *MsgStore
	hub    *Hub
	plog   *PacketLogger
	dup    *dupCache
	recent *ring

	hmu          sync.Mutex
	rfHeard      map[string]time.Time
	rfGateLog    []time.Time
	digiAliasLog []time.Time

	smu   sync.Mutex
	stats Stats

	objMu    sync.Mutex
	objNext  map[string]time.Time
	objKills []pendingKill

	beaconMu  sync.Mutex
	nextBcnRF time.Time
	nextBcnIS time.Time

	dxMu      sync.Mutex
	dx        *DXRecord
	dxPending bool // 新記録をまだ送っていない
	dxNext    time.Time

	wx wxState

	updMu       sync.Mutex
	upd         *UpdateInfo
	updNext     time.Time
	updBusy     bool
	updApplying bool
}

func NewApp(cfg Config, cfgPath string, logDir string) *App {
	return &App{
		cfgPath: cfgPath,
		started: time.Now(),
		cfg:     cfg,
		db:      NewStationDB(),
		msgs:    NewMsgStore(),
		hub:     NewHub(),
		plog:    NewPacketLogger(logDir),
		dup:     newDupCache(30 * time.Second),
		recent:  newRing(3000),
		rfHeard: map[string]time.Time{},
		objNext: map[string]time.Time{},
	}
}

func (a *App) Config() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *App) clients() (*ISClient, *TNC) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.is, a.tnc
}

// Start は設定に従って APRS-IS / TNC を起動する (再起動時は既存を停止)。
func (a *App) Start() {
	a.startMu.Lock()
	defer a.startMu.Unlock()

	// 既存の接続を止め、シリアルポート等が閉じ終わるまで待つ (待たないと再オープンが "busy" になる)
	a.mu.Lock()
	old := a.cancel
	a.cancel = nil
	a.mu.Unlock()
	if old != nil {
		old()
		done := make(chan struct{})
		go func() { a.running.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			log.Printf("前回の接続の終了待ちがタイムアウトしました")
		}
	}

	a.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	cfg := a.cfg
	a.is, a.tnc, a.davis = nil, nil, nil
	if cfg.Callsign != "" && cfg.ISEnabled {
		a.is = NewISClient(cfg, a.onISLine, a.broadcastStatus)
		a.goRun(a.is.Run, ctx)
	}
	if cfg.Callsign != "" && cfg.TNCType != "none" {
		a.tnc = NewTNC(cfg, a.onRFFrame, a.broadcastStatus)
		a.goRun(a.tnc.Run, ctx)
	}
	if cfg.WXEnabled {
		if cfg.WXSource == "davis-sdr" {
			a.davis = NewDavisRFClient(cfg, a.wxIngest, a.broadcastStatus)
		} else if cfg.WXSource == "rtl433" {
			a.davis = NewRtl433Client(cfg, a.wxIngest, a.broadcastStatus)
		} else {
			a.davis = NewDavisClient(cfg, a.wxIngest, a.broadcastStatus)
		}
		a.goRun(a.davis.Run, ctx)
	}
	a.mu.Unlock()

	a.beaconMu.Lock()
	a.nextBcnRF = time.Now().Add(15 * time.Second)
	a.nextBcnIS = time.Now().Add(15 * time.Second)
	a.beaconMu.Unlock()

	a.plog.SetEnabled(cfg.LogPackets)
	a.updateOwnStation()
	a.rescheduleObjects(nil, cfg.Objects)
	a.broadcastStatus()
}

// connChanged は接続のやり直しが必要な設定が変わったか。
func connChanged(o, n Config) bool {
	return o.Callsign != n.Callsign || o.Passcode != n.Passcode ||
		o.ISEnabled != n.ISEnabled || o.ISServer != n.ISServer || o.ISServer2 != n.ISServer2 || o.ISServer3 != n.ISServer3 || o.ISFilter != n.ISFilter ||
		o.TNCType != n.TNCType || o.SerialPort != n.SerialPort || o.Baud != n.Baud ||
		o.KissTCPAddr != n.KissTCPAddr || o.BTAddr != n.BTAddr || o.BTBridgeAddr != n.BTBridgeAddr || o.TNCInit != n.TNCInit || o.TxDelayMs != n.TxDelayMs ||
		o.WXEnabled != n.WXEnabled || o.WXSource != n.WXSource || o.WXSerialPort != n.WXSerialPort ||
		o.WXBaud != n.WXBaud || o.WXTCPAddr != n.WXTCPAddr ||
		o.WXSDRDevice != n.WXSDRDevice || o.WXSDRGain != n.WXSDRGain || o.WXSDRPPM != n.WXSDRPPM || o.WXSDRID != n.WXSDRID || o.WXRainMm != n.WXRainMm
}

func (a *App) goRun(run func(context.Context), ctx context.Context) {
	a.running.Add(1)
	go func() {
		defer a.running.Done()
		run(ctx)
	}()
}

func (a *App) ApplyConfig(cfg Config) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := saveConfig(a.cfgPath, cfg); err != nil {
		return fmt.Errorf("設定の保存に失敗しました: %w", err)
	}
	a.mu.Lock()
	old := a.cfg
	a.cfg = cfg
	a.mu.Unlock()
	a.rescheduleObjects(old.Objects, cfg.Objects)
	if connChanged(old, cfg) {
		a.Start()
	} else {
		// 接続に関係ない変更 (オブジェクト・ビーコン等) では TNC / APRS-IS をつなぎ直さない
		a.plog.SetEnabled(cfg.LogPackets)
		a.updateOwnStation()
		a.broadcastStatus()
	}
	return nil
}

// Loop は定期処理 (ビーコン・メッセージ再送・局の期限切れ) を行う。
func (a *App) Loop() {
	t := time.NewTicker(5 * time.Second)
	n := 0
	for now := range t.C {
		n++
		a.checkBeacons(now)
		a.checkObjects(now)
		a.retryMessages(now)
		a.tickDX(now)
		a.tickWX(now)
		a.tickUpdate(now)
		if n%12 == 0 { // 1 分ごと
			for _, k := range a.db.Expire(a.retention(), now) {
				a.hub.Broadcast("remove", k)
			}
			a.broadcastStatus()
			a.SaveData()
		}
	}
}

// ---------- 受信処理 ----------

func (a *App) onRFFrame(frame []byte) {
	line, err := ax25ToTNC2(frame)
	if err != nil {
		return
	}
	a.handleRF(line)
}

func (a *App) handleRF(line string) {
	a.emitPacket("RF", line)
	a.smu.Lock()
	a.stats.RFRx++
	a.smu.Unlock()

	p, err := ParseTNC2(line)
	if err != nil {
		return
	}
	cfg := a.Config()
	if isOwnCall(p.Src, cfg) {
		return // 自局の送信がデジピートされて戻ってきたもの
	}
	if p.Type == "thirdparty" && len(p.Info) > 1 {
		if ip, err := ParseTNC2(p.Info[1:]); err == nil && noRF(cfg.NoRFCalls, ip.Src) {
			return
		}
	}
	if noRF(cfg.NoRFCalls, p.Src) {
		return // 除外局: 受信を完全に無視する
	}
	a.hmu.Lock()
	a.rfHeard[strings.ToUpper(p.Src)] = time.Now()
	a.hmu.Unlock()

	a.digipeat(p, cfg)

	if p.Type == "thirdparty" {
		if ip, err := ParseTNC2(p.Info[1:]); err == nil {
			a.updateStation(ip, "IS")
			a.handleMessage(ip, "RF")
		}
	} else {
		a.updateStation(p, "RF")
		a.handleMessage(p, "RF")
		a.checkDX(p, cfg, time.Now())
	}
	if cfg.RFtoIS {
		a.gateToIS(p, cfg)
	}
}

func (a *App) onISLine(line string) {
	a.emitPacket("IS", line)
	a.smu.Lock()
	a.stats.ISRx++
	a.smu.Unlock()

	p, err := ParseTNC2(line)
	if err != nil {
		return
	}
	cfg := a.Config()
	if p.Type == "thirdparty" {
		if ip, err := ParseTNC2(p.Info[1:]); err == nil {
			p = ip
		}
	}
	if isOwnCall(p.Src, cfg) || noRF(cfg.NoRFCalls, p.Src) {
		return // 除外局: 受信を完全に無視する
	}
	a.updateStation(p, "IS")
	a.handleMessage(p, "IS")
	if cfg.IStoRF {
		a.gateToRF(p, cfg)
	}
}

func (a *App) updateStation(p *Packet, via string) {
	if p.Type == "message" || p.Type == "telemetry" {
		// メッセージだけの局は位置が無いので、既知局の更新のみ行わない
		return
	}
	cfg := a.Config()
	s := a.db.Update(p, via, time.Now(), cfg.TrackPoints)
	a.hub.Broadcast("station", s)
}

func (a *App) handleMessage(p *Packet, via string) {
	if p.Type != "message" {
		return
	}
	cfg := a.Config()
	if cfg.Callsign == "" || !sameCall(p.MsgTo, cfg.Callsign) {
		return
	}
	if p.IsAck || p.IsRej {
		if m := a.msgs.Ack(p.Src, p.MsgID, p.IsRej); m != nil {
			a.hub.Broadcast("message", m)
		}
		return
	}
	if p.ReplyAck != "" {
		if m := a.msgs.Ack(p.Src, p.ReplyAck, false); m != nil {
			a.hub.Broadcast("message", m)
		}
	}
	if p.MsgID != "" {
		// 重複受信でも ACK は毎回返す
		a.transmit(msgBody(p.Src, "ack"+p.MsgID, ""), via == "RF", via == "IS", cfg)
	}
	if m := a.msgs.Incoming(p, via); m != nil {
		log.Printf("メッセージ受信 %s: %s", m.From, m.Text)
		a.hub.Broadcast("message", m)
	}
}

// ---------- ゲート ----------

func pathHas(path []string, words ...string) bool {
	for _, d := range path {
		d = strings.TrimSuffix(strings.ToUpper(d), "*")
		for _, w := range words {
			if d == w || (w == "TCPIP" && d == "TCPXX") {
				return true
			}
		}
	}
	return false
}

// gateToIS は RF で受信したパケットを APRS-IS へ送る (IGate ルールに従う)。
func (a *App) gateToIS(p *Packet, cfg Config) {
	is, _ := a.clients()
	if is == nil || !is.Status().Verified {
		return
	}
	if pathHas(p.Path, "TCPIP", "TCPXX", "NOGATE", "RFONLY") {
		return
	}
	if len(p.Info) == 0 || p.Info[0] == '?' {
		return
	}
	hdr, info := p.Header(), p.Info
	if info[0] == '}' {
		ip, err := ParseTNC2(info[1:])
		if err != nil || pathHas(ip.Path, "TCPIP", "TCPXX", "NOGATE", "RFONLY") || len(ip.Info) == 0 {
			return
		}
		hdr, info = ip.Header(), ip.Info
	}
	if i := strings.IndexAny(info, "\r\n"); i >= 0 {
		info = info[:i]
	}
	line := hdr + ",qAR," + cfg.Callsign + ":" + info
	if is.Send(line) {
		a.smu.Lock()
		a.stats.RFtoIS++
		a.smu.Unlock()
		a.emitPacket("GATE-IS", line)
	}
}

func (a *App) heardOnRF(call string, within time.Duration) bool {
	a.hmu.Lock()
	defer a.hmu.Unlock()
	t, ok := a.rfHeard[strings.ToUpper(call)]
	return ok && time.Since(t) <= within
}

// gateToRF は APRS-IS から受けたメッセージのうち、宛先がローカル(RF)で最近受信された局のものを RF へ送る。
func (a *App) gateToRF(p *Packet, cfg Config) {
	if p.Type != "message" || p.MsgTo == "" {
		return
	}
	if noRF(cfg.NoRFCalls, p.Src) || noRF(cfg.NoRFCalls, p.MsgTo) {
		return
	}
	_, tnc := a.clients()
	if tnc == nil || !tnc.Status().Connected {
		return
	}
	to := strings.ToUpper(p.MsgTo)
	if strings.HasPrefix(to, "BLN") || strings.HasPrefix(to, "NWS") || sameCall(to, p.Src) {
		return
	}
	local := time.Duration(cfg.LocalHeardMin) * time.Minute
	if !a.heardOnRF(to, local) || a.heardOnRF(p.Src, local) {
		return
	}
	if pathHas(p.Path, "TCPXX", "NOGATE", "RFONLY") {
		return
	}
	if !a.dup.Check("rf|" + p.Src + "|" + p.Info) {
		return
	}
	// レート制限
	a.hmu.Lock()
	now := time.Now()
	kept := a.rfGateLog[:0]
	for _, t := range a.rfGateLog {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	a.rfGateLog = kept
	if len(a.rfGateLog) >= cfg.MaxIStoRFPerMin {
		a.hmu.Unlock()
		return
	}
	a.rfGateLog = append(a.rfGateLog, now)
	a.hmu.Unlock()

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

func pathSuffix(path string) string {
	if path == "" {
		return ""
	}
	return "," + path
}

// transmit は自局発のパケット本文を RF / APRS-IS に送る。
func (a *App) transmit(body string, rf, isNet bool, cfg Config) (sentRF, sentIS bool) {
	return a.transmitLines(
		cfg.Callsign+">"+toCall+pathSuffix(cfg.BeaconRFPath)+":"+body,
		cfg.Callsign+">"+toCall+",TCPIP*:"+body,
		rf, isNet)
}

// transmitLines は組み立て済みの行を RF / APRS-IS に送る。
func (a *App) transmitLines(rfLine, isLine string, rf, isNet bool) (sentRF, sentIS bool) {
	is, tnc := a.clients()
	if rf && tnc != nil && tnc.Status().Connected {
		line := rfLine
		if err := tnc.Send(line); err == nil {
			sentRF = true
			a.smu.Lock()
			a.stats.TxRF++
			a.smu.Unlock()
			a.emitPacket("TX-RF", line)
		} else {
			log.Printf("RF 送信失敗: %v", err)
		}
	}
	if isNet && is != nil && is.Status().Verified {
		line := isLine
		if is.Send(line) {
			sentIS = true
			a.smu.Lock()
			a.stats.TxIS++
			a.smu.Unlock()
			a.emitPacket("TX-IS", line)
		}
	}
	return
}

// ---------- ビーコン ----------

func formatLat(lat float64) string {
	h := byte('N')
	if lat < 0 {
		h, lat = 'S', -lat
	}
	t := int(math.Round(lat * 6000))
	return fmt.Sprintf("%02d%02d.%02d%c", t/6000, (t%6000)/100, t%100, h)
}

func formatLon(lon float64) string {
	h := byte('E')
	if lon < 0 {
		h, lon = 'W', -lon
	}
	t := int(math.Round(lon * 6000))
	return fmt.Sprintf("%03d%02d.%02d%c", t/6000, (t%6000)/100, t%100, h)
}

func beaconBody(cfg Config) string {
	ext := ""
	if cfg.PHGEnabled {
		ext = phgCode(cfg.PHGPower, cfg.PHGHeight, cfg.PHGGain, cfg.PHGDir)
	}
	return "=" + formatLat(cfg.Lat) + cfg.SymbolTable + formatLon(cfg.Lon) + cfg.Symbol + ext + cfg.Comment
}

func (a *App) updateOwnStation() {
	cfg := a.Config()
	if cfg.Callsign == "" {
		return
	}
	if p, err := ParseTNC2(cfg.Callsign + ">" + toCall + ":" + beaconBody(cfg)); err == nil {
		a.updateStation(p, "LOCAL")
	}
}

func (a *App) checkBeacons(now time.Time) {
	cfg := a.Config()
	if cfg.Callsign == "" {
		return
	}
	a.beaconMu.Lock()
	doRF := cfg.BeaconRF && !now.Before(a.nextBcnRF)
	doIS := cfg.BeaconIS && !now.Before(a.nextBcnIS)
	a.beaconMu.Unlock()
	if !doRF && !doIS {
		return
	}
	sentRF, sentIS := a.transmit(beaconBody(cfg), doRF, doIS, cfg)
	a.beaconMu.Lock()
	if sentRF {
		a.nextBcnRF = now.Add(time.Duration(cfg.BeaconRFMin) * time.Minute)
	}
	if sentIS {
		a.nextBcnIS = now.Add(time.Duration(cfg.BeaconISMin) * time.Minute)
	}
	a.beaconMu.Unlock()
	a.updateOwnStation()
}

// BeaconNow は手動ビーコン送信。
func (a *App) BeaconNow() (bool, bool) {
	cfg := a.Config()
	if cfg.Callsign == "" {
		return false, false
	}
	rf, is := a.transmit(beaconBody(cfg), true, true, cfg)
	a.updateOwnStation()
	return rf, is
}

// ---------- メッセージ送信 ----------

func (a *App) SendMessage(to, text string) (*Message, error) {
	cfg := a.Config()
	if cfg.Callsign == "" {
		return nil, fmt.Errorf("コールサインが未設定です")
	}
	to = strings.ToUpper(strings.TrimSpace(to))
	if to == "" || len(to) > 9 {
		return nil, fmt.Errorf("宛先が正しくありません")
	}
	if noRF(cfg.NoRFCalls, to) {
		return nil, fmt.Errorf("%s は「除外局」に指定されているため送信できません", to)
	}
	text = sanitizeMsgText(text)
	if text == "" {
		return nil, fmt.Errorf("本文が空です")
	}
	m := a.msgs.NewOutgoing(cfg.Callsign, to, text)
	a.hub.Broadcast("message", m)
	a.sendMsg(*m, cfg)
	return m, nil
}

func (a *App) sendMsg(m Message, cfg Config) {
	body := msgBody(m.To, m.Text, m.MsgNo)
	// 宛先が最近 RF で聞こえていれば RF、それ以外は APRS-IS (使えなければ RF)
	is, tnc := a.clients()
	isOK := is != nil && is.Status().Verified
	rfOK := tnc != nil && tnc.Status().Connected
	useRF := rfOK && (a.heardOnRF(m.To, time.Duration(cfg.LocalHeardMin)*time.Minute) || !isOK)
	sentRF, sentIS := a.transmit(body, useRF, !useRF, cfg)
	via := ""
	switch {
	case sentRF:
		via = "RF"
	case sentIS:
		via = "IS"
	default:
		via = "未送信"
	}
	if u := a.msgs.MarkSent(m.ID, via); u != nil {
		a.hub.Broadcast("message", u)
	}
}

func (a *App) retryMessages(now time.Time) {
	retry, failed := a.msgs.Due(now)
	cfg := a.Config()
	for _, m := range retry {
		a.sendMsg(m, cfg)
	}
	for _, m := range failed {
		m := m
		a.hub.Broadcast("message", &m)
	}
}

// ---------- 状態通知 ----------

func (a *App) emitPacket(dir, line string) {
	ev := PacketEvent{T: time.Now().UnixMilli(), Dir: dir, Raw: displayText(line)}
	a.recent.Add(ev)
	a.plog.Write(dir, line)
	a.hub.Broadcast("packet", ev)
}

type StatusInfo struct {
	Callsign string       `json:"callsign"`
	Version  string       `json:"version"`
	Uptime   int64        `json:"uptime"`
	IS       *ISStatus    `json:"is"`
	TNC      *TNCStatus   `json:"tnc"`
	Stats    Stats        `json:"stats"`
	Stations int          `json:"stations"`
	DX       *DXRecord    `json:"dx"`
	DXText   string       `json:"dxText,omitempty"`
	Update   *UpdateInfo  `json:"update,omitempty"`
	Davis    *DavisStatus `json:"davis,omitempty"`
}

func (a *App) Status() StatusInfo {
	is, tnc := a.clients()
	st := StatusInfo{Callsign: a.Config().Callsign, Version: softwareVersion, Uptime: int64(time.Since(a.started).Seconds())}
	if is != nil {
		s := is.Status()
		st.IS = &s
	}
	if tnc != nil {
		s := tnc.Status()
		st.TNC = &s
	}
	a.smu.Lock()
	st.Stats = a.stats
	a.smu.Unlock()
	st.Stations = len(a.db.All())
	if st.DX = a.DX(); st.DX != nil {
		st.DXText = dxStatusText(*st.DX, a.Config().DXUnit)
	}
	st.Update = a.Update()
	st.Davis = a.davisStatus()
	return st
}

func (a *App) broadcastStatus() {
	a.hub.Broadcast("status", a.Status())
}

// ---------- 補助 ----------

type dupCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]time.Time
}

func newDupCache(ttl time.Duration) *dupCache {
	return &dupCache{ttl: ttl, m: map[string]time.Time{}}
}

// Check は初見なら true を返して記録する。
func (d *dupCache) Check(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if len(d.m) > 2000 {
		for k, t := range d.m {
			if now.Sub(t) > d.ttl {
				delete(d.m, k)
			}
		}
	}
	if t, ok := d.m[key]; ok && now.Sub(t) <= d.ttl {
		return false
	}
	d.m[key] = now
	return true
}

type ring struct {
	mu   sync.Mutex
	buf  []PacketEvent
	size int
}

func newRing(n int) *ring { return &ring{size: n} }

func (r *ring) Add(e PacketEvent) {
	r.mu.Lock()
	r.buf = append(r.buf, e)
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
	}
	r.mu.Unlock()
}

func (r *ring) All() []PacketEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PacketEvent(nil), r.buf...)
}
