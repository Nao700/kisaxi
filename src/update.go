package main

// 更新の確認 (試験中)。公開リポジトリ kisaxi-release の最新リリースを GitHub API で取得し、
// 今のバージョンより新しければ画面に知らせる。ダウンロードや入れ替えは行わない (利用者が手動で行う)。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const (
	updateRepo      = "Nao700/kisaxi-release"
	updateInterval  = 24 * time.Hour   // 自動確認の間隔
	updateFirstWait = 30 * time.Second // 起動 (または有効化) から最初の確認までの待ち
)

var updateAPIURL = "https://api.github.com/repos/" + updateRepo + "/releases/latest"

type UpdateInfo struct {
	Checked int64  `json:"checked"`          // 確認した時刻 (ミリ秒)
	Current string `json:"current"`          // 今のバージョン
	Latest  string `json:"latest,omitempty"` // 公開されている最新バージョン (リリースが無ければ空)
	Newer   bool   `json:"newer"`            // 最新の方が新しい
	URL     string `json:"url,omitempty"`    // リリースのページ
	Asset   string `json:"asset,omitempty"`  // この環境用ファイルのダウンロード先 (見つかれば)
	Error   string `json:"error,omitempty"`
}

// compareVersion は "1.0.10" と "v1.0.9" のような版を数値で比べる (a>b で 1、a<b で -1)。
// "-beta" などの後ろの部分は無視する。
func compareVersion(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimLeft(v, "vV")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// platformAsset はこの環境用のリリースファイル名 (build.ps1 の出力名と同じ)。
func platformAsset() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "arm":
		arch = "armv7"
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				if s.Key == "GOARM" && strings.HasPrefix(s.Value, "6") {
					arch = "armv6"
				}
			}
		}
	}
	name := "aprsigate-" + runtime.GOOS + "-" + arch
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// fetchUpdate は url の最新リリースを取得して current と比べる。
func fetchUpdate(ctx context.Context, url, current, asset string) UpdateInfo {
	info := UpdateInfo{Checked: time.Now().UnixMilli(), Current: current}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", softwareName+"/"+current)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		info.Error = "更新の確認に失敗しました (インターネットに接続できません)"
		return info
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return info // まだリリースが公開されていない
	}
	if resp.StatusCode != http.StatusOK {
		info.Error = fmt.Sprintf("更新の確認に失敗しました (HTTP %d)", resp.StatusCode)
		return info
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil || rel.TagName == "" {
		info.Error = "更新の確認に失敗しました (応答が不正です)"
		return info
	}
	info.Latest = strings.TrimLeft(rel.TagName, "vV")
	info.URL = rel.HTMLURL
	info.Newer = compareVersion(info.Latest, current) > 0
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, asset) {
			info.Asset = a.URL
		}
	}
	return info
}

// CheckUpdate は今すぐ更新を確認し、結果を画面に知らせる。
func (a *App) CheckUpdate(ctx context.Context) UpdateInfo {
	info := fetchUpdate(ctx, updateAPIURL, softwareVersion, platformAsset())
	a.updMu.Lock()
	a.upd = &info
	a.updMu.Unlock()
	switch {
	case info.Error != "":
		log.Print(info.Error)
	case info.Newer:
		log.Printf("新しいバージョン %s が公開されています: %s", info.Latest, info.URL)
	}
	a.broadcastStatus()
	return info
}

// tickUpdate は設定で有効なとき、起動 30 秒後とその後 24 時間ごとに更新を確認する。
func (a *App) tickUpdate(now time.Time) {
	enabled := a.Config().UpdateCheck
	a.updMu.Lock()
	if !enabled {
		a.updNext = time.Time{}
		a.updMu.Unlock()
		return
	}
	if a.updNext.IsZero() {
		a.updNext = now.Add(updateFirstWait)
	}
	if now.Before(a.updNext) || a.updBusy {
		a.updMu.Unlock()
		return
	}
	a.updNext = now.Add(updateInterval)
	a.updBusy = true
	a.updMu.Unlock()
	go func() {
		a.CheckUpdate(context.Background())
		a.updMu.Lock()
		a.updBusy = false
		a.updMu.Unlock()
	}()
}

// Update は直近の確認結果を返す (未確認なら nil)。
func (a *App) Update() *UpdateInfo {
	a.updMu.Lock()
	defer a.updMu.Unlock()
	if a.upd == nil {
		return nil
	}
	u := *a.upd
	return &u
}
