package main

// 更新の確認と入れ替え。公開リポジトリ kisaxi の最新リリースを GitHub API で取得し、
// 今のバージョンより新しければ画面に知らせる。「今すぐ更新」または設定の自動更新で、
// この環境用の実行ファイルをダウンロードして入れ替え、再起動する。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const (
	updateRepo      = "Nao700/kisaxi"
	updateInterval  = 24 * time.Hour   // 自動確認の間隔
	updateFirstWait = 30 * time.Second // 起動 (または有効化) から最初の確認までの待ち
)

var updateAPIURL = "https://api.github.com/repos/" + updateRepo + "/releases/latest"

type UpdateInfo struct {
	Checked  int64  `json:"checked"`          // 確認した時刻 (ミリ秒)
	Current  string `json:"current"`          // 今のバージョン
	Latest   string `json:"latest,omitempty"` // 公開されている最新バージョン (リリースが無ければ空)
	Newer    bool   `json:"newer"`            // 最新の方が新しい
	URL      string `json:"url,omitempty"`    // リリースのページ
	Asset    string `json:"asset,omitempty"`  // この環境用ファイルのダウンロード先 (見つかれば)
	CanApply bool   `json:"canApply"`         // 画面から更新して再起動できる (この環境用ファイルが見つかった)
	Digest   string `json:"-"`                // Asset の SHA-256 ("sha256:..."、GitHub が付けていれば)
	Error    string `json:"error,omitempty"`
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
	name := "KISAXI-" + runtime.GOOS + "-" + arch
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
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
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
			info.Digest = a.Digest
			info.CanApply = true
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
	if info.Newer && info.Asset != "" && a.Config().UpdateAuto {
		if err := a.ApplyUpdate(ctx); err != nil {
			log.Printf("自動更新に失敗しました: %v", err)
		}
	}
	return info
}

// downloadBinary は url のファイルを dir に保存し、パスを返す。
// https 以外・極端に小さい/大きいファイル・この環境用の実行ファイルでないもの・SHA-256 が合わないものは受け付けない。
func downloadBinary(ctx context.Context, url, digest, dir string) (string, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1") {
		return "", errors.New("ダウンロード先が https ではありません")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", softwareName+"/"+softwareVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ダウンロードできません: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ダウンロードに失敗しました (HTTP %d)", resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, ".update-*")
	if err != nil {
		return "", fmt.Errorf("実行ファイルのフォルダに書き込めません: %w", err)
	}
	name := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBinarySize+1))
	if err != nil {
		return "", fmt.Errorf("ダウンロードが途中で止まりました: %w", err)
	}
	if n < minBinarySize || n > maxBinarySize {
		return "", fmt.Errorf("ダウンロードしたファイルの大きさが不正です (%d バイト)", n)
	}
	if want, found := strings.CutPrefix(digest, "sha256:"); found && !strings.EqualFold(want, hex.EncodeToString(h.Sum(nil))) {
		return "", errors.New("ダウンロードしたファイルのチェックサムが一致しません")
	}
	head := make([]byte, 4)
	if _, err := f.ReadAt(head, 0); err != nil || !validBinaryHeader(head) {
		return "", errors.New("ダウンロードしたファイルがこの環境用の実行ファイルではありません")
	}
	if err := f.Chmod(0o755); err != nil && runtime.GOOS != "windows" {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

const (
	minBinarySize = 1 << 20
	maxBinarySize = 200 << 20
)

// validBinaryHeader は先頭 4 バイトが実行ファイルの形式 (Windows: PE、Linux: ELF) か確認する。
func validBinaryHeader(h []byte) bool {
	if runtime.GOOS == "windows" {
		return bytes.HasPrefix(h, []byte("MZ"))
	}
	return bytes.HasPrefix(h, []byte("\x7fELF"))
}

// replaceExecutable は現在の実行ファイル exe を newPath で置き換える。
// Windows は実行中のファイルを上書きできないため、いったん .old に退避してから置く (.old は次回起動時に消す)。
func replaceExecutable(exe, newPath string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(newPath, exe)
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe) // 元に戻す
		return err
	}
	return nil
}

// cleanupOldBinary は前回の更新で退避した古い実行ファイルを消す (起動時に呼ぶ)。
func cleanupOldBinary() {
	if exe, err := currentExecutable(); err == nil {
		os.Remove(exe + ".old")
	}
}

func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// ApplyUpdate は直近の確認で見つかった新しい版をダウンロードして入れ替え、再起動する。
// 成功すると少し後にプロセスが入れ替わるため、戻ってきたときは入れ替え済み (再起動待ち) を意味する。
func (a *App) ApplyUpdate(ctx context.Context) error {
	info := a.Update()
	if info == nil || !info.Newer {
		return errors.New("更新できる新しいバージョンがありません")
	}
	if info.Asset == "" {
		return errors.New("この環境用のファイルがリリースに見つかりません。公開ページから手動で入れ替えてください")
	}
	a.updMu.Lock()
	if a.updApplying {
		a.updMu.Unlock()
		return errors.New("更新中です")
	}
	a.updApplying = true
	a.updMu.Unlock()
	fail := func(err error) error {
		a.updMu.Lock()
		a.updApplying = false
		a.updMu.Unlock()
		return err
	}
	exe, err := currentExecutable()
	if err != nil {
		return fail(err)
	}
	log.Printf("Ver.%s をダウンロードします", info.Latest)
	tmp, err := downloadBinary(ctx, info.Asset, info.Digest, filepath.Dir(exe))
	if err != nil {
		return fail(err)
	}
	if err := replaceExecutable(exe, tmp); err != nil {
		os.Remove(tmp)
		return fail(fmt.Errorf("実行ファイルを入れ替えられません: %w", err))
	}
	log.Printf("Ver.%s に入れ替えました。再起動します", info.Latest)
	go func() {
		time.Sleep(time.Second) // 画面に結果を返してから
		a.SaveData()
		a.SaveWX()
		restartSelf(exe)
	}()
	return nil
}

// restartSelf は入れ替えた実行ファイルを同じ引数で起動し直して、このプロセスを終了する。
// systemd 配下 (INVOCATION_ID あり) では、サービス側の Restart= に任せて終了だけする。
func restartSelf(exe string) {
	if os.Getenv("INVOCATION_ID") == "" {
		args := append([]string{"-after-update", "-nobrowser"}, os.Args[1:]...)
		cmd := exec.Command(exe, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		detach(cmd)
		if err := cmd.Start(); err != nil {
			log.Printf("再起動できませんでした。手動で起動してください: %v", err)
			return
		}
	}
	os.Exit(0)
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
