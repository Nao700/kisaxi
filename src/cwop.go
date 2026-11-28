package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// CWOP (Citizen Weather Observer Program) への気象データ送信。
// CWOP のサーバーは APRS-IS と同じ接続方式で、気象パケットを 1 本送って切断すればよい。
// 送信間隔は 5 分以上 (既定 10 分)。

const (
	cwopDefaultServer = "cwop.aprs.net:14580"
	cwopMinMin        = 5
	cwopDefaultMin    = 10
)

// cwopIDRe は CWOP 専用 ID (CW1234 / DW1234 / EW1234 / FW1234 など、パスコード不要で -1 で接続できる)
var cwopIDRe = regexp.MustCompile(`^[A-Z]W\d{3,5}$`)

// cwopID は CWOP に登録した ID。未指定なら気象局のコールサイン-SSID。
func cwopID(cfg Config) string {
	if id := strings.ToUpper(strings.TrimSpace(cfg.WXCWOPID)); id != "" {
		return id
	}
	return wxCallsign(cfg)
}

// cwopPass は CWOP ログインのパスコード。未指定なら CWOP 専用 ID は -1、アマチュア局のコールサインは自局のパスコード。
func cwopPass(cfg Config, id string) string {
	if p := strings.TrimSpace(cfg.WXCWOPPass); p != "" {
		return p
	}
	if cwopIDRe.MatchString(id) {
		return "-1"
	}
	return cfg.Passcode
}

func cwopServer(cfg Config) string {
	s := strings.TrimSpace(cfg.WXCWOPServer)
	if s == "" {
		s = cwopDefaultServer
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		s = net.JoinHostPort(strings.Trim(s, "[]"), "14580")
	}
	return s
}

// cwopLine は CWOP に送るパケット行を作る (送信元は CWOP の ID)。
func cwopLine(id, body string) string {
	return id + ">" + toCall + ",TCPIP*:" + body
}

// sendCWOP は CWOP サーバーへつないで気象パケットを 1 本送る。
func sendCWOP(cfg Config, id, line string) error {
	conn, err := net.DialTimeout("tcp", cwopServer(cfg), 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	rd := bufio.NewReader(conn)
	if _, err := rd.ReadString('\n'); err != nil { // サーバーのバナー
		return err
	}
	login := fmt.Sprintf("user %s pass %s vers %s %s\r\n", id, cwopPass(cfg, id), softwareName, softwareVersion)
	if _, err := conn.Write([]byte(login)); err != nil {
		return err
	}
	for i := 0; i < 5; i++ {
		l, err := rd.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.Contains(l, "logresp") {
			if strings.Contains(l, "unverified") {
				return errors.New("CWOP のログインが認証されませんでした (ID またはパスコードを確認してください)")
			}
			break
		}
	}
	if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
		return err
	}
	time.Sleep(time.Second) // 送り切ってから切断する
	return nil
}
