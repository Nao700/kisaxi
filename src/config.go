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

	ISEnabled bool   `json:"isEnabled"`
	ISServer  string `json:"isServer"`
	ISFilter  string `json:"isFilter"`

	TNCType     string `json:"tncType"` // none, kiss-serial, kiss-tcp
	SerialPort  string `json:"serialPort"`
	Baud        int    `json:"baud"`
	KissTCPAddr string `json:"kissTcpAddr"`
	TNCInit     string `json:"tncInit"` // KISS モード移行前に送るコマンド (改行区切り)
	TxDelayMs   int    `json:"txDelayMs"`

	RFtoIS         bool   `json:"rfToIs"`
	IStoRF         bool   `json:"isToRf"`
	LocalHeardMin  int    `json:"localHeardMin"`
	IStoRFPath     string `json:"isToRfPath"`
	MaxIStoRFPerMin int   `json:"maxIsToRfPerMin"`

	DigiEnabled bool `json:"digiEnabled"` // デジピータ (WIDE1-1・自局コール宛を中継)
	DigiWide    bool `json:"digiWide"`    // WIDE2-n も中継する

	BeaconRF       bool   `json:"beaconRf"`
	BeaconRFMin    int    `json:"beaconRfMin"`
	BeaconRFPath   string `json:"beaconRfPath"`
	BeaconIS       bool   `json:"beaconIs"`
	BeaconISMin    int    `json:"beaconIsMin"`

	StationExpireMin int    `json:"stationExpireMin"`
	TrackPoints      int    `json:"trackPoints"`
	LogPackets       bool   `json:"logPackets"`
	HTTPListen       string `json:"httpListen"`
	OpenBrowser      bool   `json:"openBrowser"`

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
