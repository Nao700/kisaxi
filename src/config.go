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
	ISServer  string `json:"isServer"`
	ISFilter  string `json:"isFilter"`

	TNCType     string `json:"tncType"` // none, kiss-serial, kiss-tcp
	SerialPort  string `json:"serialPort"`
	Baud        int    `json:"baud"`
	KissTCPAddr string `json:"kissTcpAddr"`
	TNCInit     string `json:"tncInit"` // KISS モード移行前に送るコマンド (改行区切り)
	TxDelayMs   int    `json:"txDelayMs"`

	RFtoIS          bool   `json:"rfToIs"`
	IStoRF          bool   `json:"isToRf"`
	LocalHeardMin   int    `json:"localHeardMin"`
	IStoRFPath      string `json:"isToRfPath"`
	MaxIStoRFPerMin int    `json:"maxIsToRfPerMin"`

	DigiEnabled bool `json:"digiEnabled"` // デジピータ (WIDE1-1・自局コール宛を中継)
	DigiWide    bool `json:"digiWide"`    // WIDE2-n も中継する

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
	LogPackets       bool   `json:"logPackets"`
	HTTPListen       string `json:"httpListen"`
	OpenBrowser      bool   `json:"openBrowser"`

	// ウェザーステーション (Davis)。気象データを読み取り、別 SSID で APRS WX として送信する
	WXEnabled    bool   `json:"wxEnabled"`
	WXSource     string `json:"wxSource"` // davis-serial (USB / シリアルのロガー), davis-tcp (WeatherLinkIP)
	WXSerialPort string `json:"wxSerialPort"`
	WXBaud       int    `json:"wxBaud"`
	WXTCPAddr    string `json:"wxTcpAddr"`
	WXCall       string `json:"wxCall"` // WX 局のコールサイン-SSID (空欄なら自局のコールに -13 を付ける)
	WXIS         bool   `json:"wxIs"`
	WXISMin      int    `json:"wxIsMin"`
	WXRF         bool   `json:"wxRf"`
	WXRFMin      int    `json:"wxRfMin"`

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
		Comment:          "APRS I-Gate",
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
		WXBaud:           19200,
		WXIS:             true,
		WXISMin:          5,
		WXRF:             false,
		WXRFMin:          30,
		StationExpireMin: 60,
		TrackPoints:      300,
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
	if c.WXSource != "davis-tcp" {
		c.WXSource = "davis-serial"
	}
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
	switch c.TNCType {
	case "none", "kiss-serial", "kiss-tcp":
	default:
		return errors.New("TNC 種別が正しくありません")
	}
	return validateObjects(c)
}

// configPath は設定ファイルの既定パスを返す。実行ファイルと同じフォルダに書き込めればそこを使う (ポータブル運用)。
func configPath() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		test := filepath.Join(dir, ".write_test")
		if f, err := os.Create(test); err == nil {
			f.Close()
			os.Remove(test)
			return filepath.Join(dir, "aprsigate.json")
		}
	}
	if dir, err := os.UserConfigDir(); err == nil {
		dir = filepath.Join(dir, "aprsigate")
		os.MkdirAll(dir, 0o755)
		return filepath.Join(dir, "aprsigate.json")
	}
	return "aprsigate.json"
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
