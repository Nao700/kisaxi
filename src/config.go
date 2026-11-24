package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Callsign    string  `json:"callsign"`
	Passcode    string  `json:"passcode"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	SymbolTable string  `json:"symbolTable"`
	Symbol      string  `json:"symbol"`
	Comment     string  `json:"comment"`

	PHGEnabled  bool    `json:"phgEnabled"`  // ビーコンに PHG (出力・アンテナ高・利得・指向) を付ける
	PHGPower    float64 `json:"phgPower"`    // 送信出力 (W)
	PHGHeight   float64 `json:"phgHeight"`   // アンテナ高 (m, 平均地表高)
	PHGGain     float64 `json:"phgGain"`     // アンテナ利得 (dBi)
	PHGDir      int     `json:"phgDir"`      // 指向 0=無指向, 45〜360 (度)
	PHGRangeMap bool    `json:"phgRangeMap"` // 地図に通達範囲を表示

	ISEnabled bool   `json:"isEnabled"`
	ISServer  string `json:"isServer"`  // 主
	ISServer2 string `json:"isServer2"` // 副 (主につながらないとき使う。空欄なら無し)
	ISServer3 string `json:"isServer3"` // 予備 (副もだめなとき使う。空欄なら無し)
	ISFilter  string `json:"isFilter"`

	TNCType      string `json:"tncType"` // none, kiss-serial, kiss-tcp, kiss-bt
	SerialPort   string `json:"serialPort"`
	Baud         int    `json:"baud"`
	KissTCPAddr  string `json:"kissTcpAddr"`
	BTAddr       string `json:"btAddr"`       // kiss-bt: 接続先 Bluetooth アドレス (Android ブリッジ経由)
	BTName       string `json:"btName"`       // kiss-bt: 表示用の機器名
	BTBridgeAddr string `json:"btBridgeAddr"` // kiss-bt: ブリッジの HTTP アドレス (空=127.0.0.1:8074)
	TNCInit      string `json:"tncInit"`      // KISS モード移行前に送るコマンド (改行区切り)
	TxDelayMs    int    `json:"txDelayMs"`

	RFtoIS          bool   `json:"rfToIs"`
	IStoRF          bool   `json:"isToRf"`
	LocalHeardMin   int    `json:"localHeardMin"`
	IStoRFPath      string `json:"isToRfPath"`
	MaxIStoRFPerMin int    `json:"maxIsToRfPerMin"`

	NoRFCalls string `json:"noRfCalls"` // 除外局: 受信を無視し、RF へも送らない (カンマ区切り。JA1ABC は全 SSID、JA1ABC-9 は完全一致、JA1* は前方一致)

	DigiEnabled bool `json:"digiEnabled"` // デジピータ (WIDE1-1・自局コール宛を中継)
	DigiWide    bool `json:"digiWide"`    // WIDE2-n も中継する

	DigiAliases      string `json:"digiAliases"`      // UIflood のエイリアス (例: KN,NS,AC)。KNn-N を自局コールを入れず残りホップだけ減らして中継する。空欄で無効
	DigiTraceAliases string `json:"digiTraceAliases"` // UItrace のエイリアス (例: TRACE)。TRACEn-N を自局コールを残して中継する。空欄で無効
	IStoRFCalls      string `json:"isToRfCalls"`      // IS→RF 送出する位置情報を局で絞る (JA1ABC は全 SSID、JA1ABC-9 は完全一致、JA1* は前方一致。カンマ区切り)
	IStoRFComments   string `json:"isToRfComments"`   // ポジションコメントに含まれる文字列で送出する (含む文字列。カンマ区切り、大小文字無視)
	DigiAliasPerMin  int    `json:"digiAliasPerMin"`  // UIflood/UItrace 中継の上限 (件/分)

	BeaconRF     bool   `json:"beaconRf"`
	BeaconRFMin  int    `json:"beaconRfMin"`
	BeaconRFPath string `json:"beaconRfPath"`
	BeaconIS     bool   `json:"beaconIs"`
	BeaconISMin  int    `json:"beaconIsMin"`

	DXEnabled bool   `json:"dxEnabled"` // 最長距離 (DX) 更新時にステータスを送る
	DXRF      bool   `json:"dxRf"`      // DX ステータスを RF にも送る
	DXReset   string `json:"dxReset"`   // day (毎日 0 時) / hour (毎時 0 分)
	DXUnit    string `json:"dxUnit"`    // km / mi

	UpdateCheck bool `json:"updateCheck"` // 起動時と 24 時間ごとに新しいバージョンを確認 (既定オフ)
	UpdateAuto  bool `json:"updateAuto"`  // 新しい版が見つかったら自動でダウンロード・入れ替え・再起動 (既定オフ、UpdateCheck が前提)

	StationExpireMin int    `json:"stationExpireMin"`
	TrackPoints      int    `json:"trackPoints"`
	TrackSplit       string `json:"trackSplit"`  // 軌跡の区切り: day (日付が変わったら) / gap (TrackGapMin 分あいたら) / none
	TrackGapMin      int    `json:"trackGapMin"` // TrackSplit=gap のときの間隔 (分)
	LogPackets       bool   `json:"logPackets"`
	HTTPListen       string `json:"httpListen"`
	OpenBrowser      bool   `json:"openBrowser"`

	// ウェザーステーション (Davis)。気象データを読み取り、別 SSID で APRS WX として送信する
	WXEnabled    bool    `json:"wxEnabled"`
	WXSource     string  `json:"wxSource"` // davis-serial (USB / シリアルのロガー), davis-tcp (WeatherLinkIP), davis-sdr (RTL-SDR 直接受信), rtl433 (β版)
	WXSerialPort string  `json:"wxSerialPort"`
	WXBaud       int     `json:"wxBaud"`
	WXTCPAddr    string  `json:"wxTcpAddr"`
	WXSDRDevice  int     `json:"wxSdrDevice"` // davis-sdr: RTL-SDR の番号 (0 から)
	WXSDRGain    float64 `json:"wxSdrGain"`   // davis-sdr: ゲイン dB (0 = 自動)
	WXSDRPPM     int     `json:"wxSdrPpm"`    // davis-sdr: 周波数補正 ppm
	WXSDRID      int     `json:"wxSdrId"`     // davis-sdr: 受信する ISS の ID (1〜8)
	WXRainMm     float64 `json:"wxRainMm"`    // davis-sdr: 雨量計 1 回の転倒の雨量 mm (国内版 0.2 / 米国版 0.254)
	WXRtlCmd     string  `json:"wxRtlCmd"`    // rtl433 のとき起動するコマンド (JSON を標準出力へ出す)
	WXRtlModel   string  `json:"wxRtlModel"`  // 使うセンサーの model (部分一致、空欄ですべて)
	WXRtlID      string  `json:"wxRtlId"`     // 使うセンサーの id (空欄ですべて)
	WXCall       string  `json:"wxCall"`      // WX 局のコールサイン-SSID (空欄なら自局のコールに -13 を付ける)
	WXIS         bool    `json:"wxIs"`
	WXISMin      int     `json:"wxIsMin"`
	WXRF         bool    `json:"wxRf"`
	WXRFMin      int     `json:"wxRfMin"`

	Objects []ObjectDef `json:"objects"`
}

func defaultConfig() Config {
	return Config{
		Callsign:         "",
		Passcode:         "-1",
		Lat:              35.6812,
		Lon:              139.7671,
		SymbolTable:      "I",
		Symbol:           "&",
		Comment:          "KISAXI",
		PHGEnabled:       false,
		PHGPower:         10,
		PHGHeight:        10,
		PHGGain:          3,
		PHGDir:           0,
		PHGRangeMap:      true,
		ISEnabled:        true,
		ISServer:         "rotate.aprs2.net:14580",
		ISFilter:         "m/50",
		TNCType:          "none",
		Baud:             9600,
		KissTCPAddr:      "127.0.0.1:8001",
		RFtoIS:           true,
		IStoRF:           true,
		LocalHeardMin:    30,
		IStoRFPath:       "WIDE1-1",
		MaxIStoRFPerMin:  10,
		DigiAliasPerMin:  10,
		BeaconRF:         false,
		BeaconRFMin:      30,
		BeaconRFPath:     "WIDE1-1",
		BeaconIS:         true,
		BeaconISMin:      20,
		DXEnabled:        true,
		DXRF:             false,
		DXReset:          "day",
		DXUnit:           "km",
		UpdateCheck:      false,
		WXSource:         "davis-serial",
		WXSDRID:          1,
		WXRainMm:         0.2,
		WXBaud:           19200,
		WXRtlCmd:         "rtl_433 -F json",
		WXIS:             true,
		WXISMin:          5,
		WXRF:             false,
		WXRFMin:          30,
		StationExpireMin: 60,
		TrackPoints:      300,
		TrackSplit:       "day",
		TrackGapMin:      30,
		LogPackets:       true,
		HTTPListen:       "127.0.0.1:8073",
		OpenBrowser:      true,
	}
}

func (c *Config) Normalize() {
	d := defaultConfig()
	c.Callsign = strings.ToUpper(strings.TrimSpace(c.Callsign))
	c.Passcode = strings.TrimSpace(c.Passcode)
	if c.Passcode == "" {
		c.Passcode = "-1"
	}
	if len(c.SymbolTable) != 1 {
		c.SymbolTable = d.SymbolTable
	}
	if len(c.Symbol) != 1 {
		c.Symbol = d.Symbol
	}
	if c.ISServer == "" {
		c.ISServer = d.ISServer
	}
	if c.Baud <= 0 {
		c.Baud = d.Baud
	}
	if c.LocalHeardMin <= 0 {
		c.LocalHeardMin = d.LocalHeardMin
	}
	if c.DigiAliasPerMin <= 0 {
		c.DigiAliasPerMin = d.DigiAliasPerMin
	}
	if c.MaxIStoRFPerMin <= 0 {
		c.MaxIStoRFPerMin = d.MaxIStoRFPerMin
	}
	if c.BeaconRFMin < 5 {
		c.BeaconRFMin = 5
	}
	if c.BeaconISMin < 5 {
		c.BeaconISMin = 5
	}
	if c.StationExpireMin <= 0 {
		c.StationExpireMin = d.StationExpireMin
	}
	if c.TrackPoints <= 0 {
		c.TrackPoints = d.TrackPoints
	}
	if c.TrackSplit != "gap" && c.TrackSplit != "none" {
		c.TrackSplit = "day"
	}
	if c.TrackGapMin <= 0 {
		c.TrackGapMin = d.TrackGapMin
	}
	if c.HTTPListen == "" {
		c.HTTPListen = d.HTTPListen
	}
	if c.PHGDir%45 != 0 || c.PHGDir < 0 || c.PHGDir > 360 {
		c.PHGDir = 0
	}
	if c.DXReset != "hour" {
		c.DXReset = "day"
	}
	if c.DXUnit != "mi" {
		c.DXUnit = "km"
	}
	if c.WXSource != "davis-tcp" && c.WXSource != "davis-sdr" && c.WXSource != "rtl433" {
		c.WXSource = "davis-serial"
	}
	if c.WXSDRID < 1 || c.WXSDRID > 8 {
		c.WXSDRID = d.WXSDRID
	}
	if c.WXSDRDevice < 0 {
		c.WXSDRDevice = 0
	}
	if c.WXSDRGain < 0 || c.WXSDRGain > 50 {
		c.WXSDRGain = 0
	}
	if c.WXSDRPPM < -200 || c.WXSDRPPM > 200 {
		c.WXSDRPPM = 0
	}
	if c.WXRainMm != 0.2 && c.WXRainMm != 0.254 {
		c.WXRainMm = d.WXRainMm
	}
	c.WXRtlCmd = strings.TrimSpace(c.WXRtlCmd)
	if c.WXRtlCmd == "" {
		c.WXRtlCmd = d.WXRtlCmd
	}
	c.WXRtlModel = strings.TrimSpace(c.WXRtlModel)
	c.WXRtlID = strings.TrimSpace(c.WXRtlID)
	if c.WXBaud <= 0 {
		c.WXBaud = d.WXBaud
	}
	if c.WXISMin < 5 {
		c.WXISMin = 5
	}
	if c.WXRFMin < 5 {
		c.WXRFMin = 5
	}
	c.WXCall = strings.ToUpper(strings.TrimSpace(c.WXCall))
	c.WXSerialPort = strings.TrimSpace(c.WXSerialPort)
	c.WXTCPAddr = strings.TrimSpace(c.WXTCPAddr)
	c.NoRFCalls = strings.ToUpper(strings.Join(strings.FieldsFunc(c.NoRFCalls, func(r rune) bool { return r == ',' || r == ' ' || r == '、' || r == '\n' }), ","))
	c.DigiTraceAliases = strings.ToUpper(strings.Join(splitAliases(c.DigiTraceAliases), ","))
	c.DigiAliases = strings.ToUpper(strings.Join(strings.FieldsFunc(c.DigiAliases, func(r rune) bool { return r == ',' || r == ' ' || r == '、' || r == '\n' }), ","))
	c.IStoRFCalls = strings.ToUpper(strings.Join(strings.FieldsFunc(c.IStoRFCalls, func(r rune) bool { return r == ',' || r == ' ' || r == '、' || r == '\n' }), ","))
	c.IStoRFComments = strings.Join(splitComments(c.IStoRFComments), ",")
	c.IStoRFPath = strings.ToUpper(strings.TrimSpace(c.IStoRFPath))
	c.BeaconRFPath = strings.ToUpper(strings.TrimSpace(c.BeaconRFPath))
}

func (c *Config) Validate() error {
	if c.Callsign != "" {
		if _, err := encodeAddr(c.Callsign, false, false); err != nil {
			return errors.New("コールサインが正しくありません (例: JA1XXX-10)")
		}
	}
	if c.Lat < -90 || c.Lat > 90 || c.Lon < -180 || c.Lon > 180 {
		return errors.New("緯度・経度の範囲が正しくありません")
	}
	if c.PHGEnabled {
		if c.PHGPower < 0 || c.PHGHeight < 0 || c.PHGGain < 0 {
			return errors.New("出力・アンテナ高・利得は 0 以上で入力してください")
		}
		if len(c.Comment) > 36 {
			return errors.New("PHG を付けるときはコメントを 36 文字以内にしてください")
		}
		if strings.HasPrefix(c.Comment, "PHG") {
			return errors.New("PHG を設定欄で付けるときは、コメント先頭の PHG を削除してください")
		}
	}
	if c.WXCall != "" {
		if _, err := encodeAddr(c.WXCall, false, false); err != nil {
			return errors.New("気象局のコールサインが正しくありません (例: JA1XXX-13)")
		}
		if c.Callsign != "" && sameCall(c.WXCall, c.Callsign) {
			return errors.New("気象局のコールサインは自局とは別の SSID にしてください (例: JA1XXX-13)")
		}
	}
	for _, p := range []string{c.IStoRFPath, c.BeaconRFPath} {
		if p == "" {
			continue
		}
		parts := strings.Split(p, ",")
		if len(parts) > 8 {
			return errors.New("パスのデジピータ数が多すぎます")
		}
		for _, d := range parts {
			if _, err := encodeAddr(d, false, false); err != nil {
				return errors.New("パスの指定が正しくありません: " + d)
			}
		}
	}
	for _, al := range append(splitAliases(c.DigiAliases), splitAliases(c.DigiTraceAliases)...) {
		if err := checkAlias(al); err != nil {
			return err
		}
	}
	switch c.TNCType {
	case "none", "kiss-serial", "kiss-tcp", "kiss-bt":
	default:
		return errors.New("TNC 種別が正しくありません")
	}
	return validateObjects(c)
}

// legacyName は旧名称 (aprsigate) のファイルが既にあればそちらを、無ければ新名称のファイルを返す。
// 試用版からの移行で、設定や受信履歴が引き継がれるようにする。
func legacyName(dir, newName, oldName string) string {
	n := filepath.Join(dir, newName)
	if _, err := os.Stat(n); err == nil {
		return n
	}
	if o := filepath.Join(dir, oldName); fileExists(o) {
		return o
	}
	return n
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// configPath は設定ファイルの既定パスを返す。実行ファイルと同じフォルダに書き込めればそこを使う (ポータブル運用)。
func configPath() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		test := filepath.Join(dir, ".write_test")
		if f, err := os.Create(test); err == nil {
			f.Close()
			os.Remove(test)
			return legacyName(dir, "kisaxi.json", "aprsigate.json")
		}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		old := filepath.Join(dir, "aprsigate")
		dir = filepath.Join(dir, "kisaxi")
		if fileExists(filepath.Join(old, "aprsigate.json")) && !fileExists(filepath.Join(dir, "kisaxi.json")) {
			return filepath.Join(old, "aprsigate.json")
		}
		os.MkdirAll(dir, 0o755)
		return filepath.Join(dir, "kisaxi.json")
	}
	return "kisaxi.json"
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return defaultConfig(), err
	}
	cfg.Normalize()
	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
