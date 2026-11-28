package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFiles embed.FS

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("ブラウザを起動できませんでした。手動で %s を開いてください", u)
	}
}

func main() {
	cfgFlag := flag.String("config", "", "設定ファイルのパス (既定: 実行ファイルと同じフォルダの kisaxi.json)")
	listen := flag.String("listen", "", "Web UI の待ち受けアドレス (例: 0.0.0.0:8073)")
	noBrowser := flag.Bool("nobrowser", false, "起動時にブラウザを開かない")
	afterUpdate := flag.Bool("after-update", false, "更新後の再起動 (内部用: 古いプロセスが終わるまで待ち受けを再試行する)")
	flag.Parse()
	if *afterUpdate {
		closeInheritedPorts()
	}
	cleanupOldBinary()

	path := *cfgFlag
	if path == "" {
		path = configPath()
	}
	cfg, err := loadConfig(path)
	if err != nil {
		log.Printf("設定ファイルの読み込みに失敗しました (%v)。既定値で起動します", err)
	}
	if *listen != "" {
		cfg.HTTPListen = *listen
	}
	logDir := filepath.Join(filepath.Dir(path), "logs")

	fmt.Printf("%s %s  (設定: %s)\n", softwareName, softwareVersion, path)

	// 先に Web UI の待ち受けを確保する。更新後の再起動 (-after-update) では、古いプロセスが
	// 終わるまでここで待つ。古いプロセスはシリアルポート (TNC) も持っているので、
	// 待ち受けが取れた時点で TNC のポートも空いており、「Serial port busy」にならない。
	ln, err := net.Listen("tcp", cfg.HTTPListen)
	for i := 0; err != nil && *afterUpdate && i < 40; i++ {
		time.Sleep(500 * time.Millisecond)
		ln, err = net.Listen("tcp", cfg.HTTPListen)
	}
	if err != nil {
		log.Fatalf("Web UI を %s で開始できません: %v", cfg.HTTPListen, err)
	}

	if *afterUpdate {
		// Bluetooth (rfcomm) の切断がカーネル側で終わるのを待ってから TNC のポートを開く
		time.Sleep(2 * time.Second)
	}

	app := NewApp(cfg, path, logDir)
	app.LoadData()
	app.LoadWX()
	app.Start()
	go app.Loop()

	static, _ := fs.Sub(webFiles, "web")
	host := cfg.HTTPListen
	if strings.HasPrefix(host, "0.0.0.0:") || strings.HasPrefix(host, ":") {
		host = "127.0.0.1:" + host[strings.LastIndexByte(host, ':')+1:]
	}
	u := "http://" + host + "/"
	fmt.Printf("Web UI: %s   (終了するには Ctrl+C またはこのウィンドウを閉じてください)\n", u)
	if cfg.OpenBrowser && !*noBrowser {
		openBrowser(u)
	}

	go func() {
		sig := make(chan os.Signal, 1)
		// Ctrl+C / ウィンドウを閉じる (Windows) / systemd の停止 (Linux) で受信データを保存して終了
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fmt.Println("受信データを保存して終了します")
		app.SaveData()
		app.SaveWX()
		os.Exit(0)
	}()
	log.Fatal(http.Serve(ln, app.Handler(static)))
}
