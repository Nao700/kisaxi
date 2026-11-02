package main

// Android 向け Bluetooth ブリッジ (KISAXI Android ラッパーアプリ) との連携。
// Android では /dev/rfcomm* が使えないため、ラッパーアプリがペアリング済み機器へ SPP 接続し、
// KISS を 127.0.0.1 の TCP に中継する。この側は HTTP で機器一覧の取得と接続指示だけを行う。
//
//	GET  /devices                 -> [{"name":"TH-D75","addr":"AA:BB:CC:DD:EE:FF"}, ...]
//	POST /connect?addr=AA:BB:..   -> {"ok":true,"kiss":"127.0.0.1:8075"} / {"ok":false,"error":"..."}
//	POST /disconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const defaultBTBridgeAddr = "127.0.0.1:8074"

type BTDevice struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

var btClient = &http.Client{Timeout: 20 * time.Second}

func btBridgeBase(addr string) string {
	if addr == "" {
		addr = defaultBTBridgeAddr
	}
	return "http://" + addr
}

func btRequest(ctx context.Context, method, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	resp, err := btClient.Do(req)
	if err != nil {
		return errors.New("Bluetooth ブリッジに接続できません (Android アプリが起動していません)")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("Bluetooth ブリッジのエラー: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func listBTDevices(ctx context.Context, bridgeAddr string) ([]BTDevice, error) {
	devs := []BTDevice{}
	err := btRequest(ctx, http.MethodGet, btBridgeBase(bridgeAddr)+"/devices", &devs)
	if devs == nil {
		devs = []BTDevice{}
	}
	return devs, err
}

// openBT は機器への接続をブリッジに依頼し、中継用の KISS TCP に接続して返す。
func openBT(ctx context.Context, cfg Config) (io.ReadWriteCloser, error) {
	if cfg.BTAddr == "" {
		return nil, errors.New("Bluetooth 機器が未選択です")
	}
	var res struct {
		KISS string `json:"kiss"`
	}
	u := btBridgeBase(cfg.BTBridgeAddr) + "/connect?addr=" + url.QueryEscape(cfg.BTAddr)
	if err := btRequest(ctx, http.MethodPost, u, &res); err != nil {
		return nil, err
	}
	if res.KISS == "" {
		return nil, errors.New("Bluetooth ブリッジの応答が不正です")
	}
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, "tcp", res.KISS)
}
