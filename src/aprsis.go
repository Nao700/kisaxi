package main

// APRS-IS サーバーへの接続 (自動再接続付き)

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type ISStatus struct {
	Connected bool   `json:"connected"`
	Verified  bool   `json:"verified"`
	Server    string `json:"server"`
	Error     string `json:"error"`
}

type ISClient struct {
	servers  []string // 主・副・予備の順
	login    string
	onLine   func(string)
	onStatus func()

	out chan string
	mu  sync.Mutex
	st  ISStatus
}

// effectiveISFilter は APRS-IS に送るフィルターを返す。
// m/距離 は「サーバーが知っている自局の位置から」の範囲だが、パスコード -1 (受信専用) では位置パケットを
// 送れず、サーバーが位置を知らないため何も流れてこない。受信専用で自局の緯度経度が設定済みなら、
// m/距離 を r/緯度/経度/距離 に置き換える。
func effectiveISFilter(cfg Config) string {
	f := strings.TrimSpace(cfg.ISFilter)
	if f == "" || cfg.Passcode != "-1" || (cfg.Lat == 0 && cfg.Lon == 0) {
		return f
	}
	parts := strings.Fields(f)
	for i, p := range parts {
		if strings.HasPrefix(p, "m/") && len(p) > 2 {
			parts[i] = fmt.Sprintf("r/%.4f/%.4f/%s", cfg.Lat, cfg.Lon, p[2:])
		}
	}
	return strings.Join(parts, " ")
}

// isServers は設定から接続先 (主・副・予備) を順番に取り出す。空欄と重複は除き、ポート省略時は 14580 を補う。
func isServers(cfg Config) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range []string{cfg.ISServer, cfg.ISServer2, cfg.ISServer3} {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			s = net.JoinHostPort(strings.Trim(s, "[]"), "14580")
		}
		if k := strings.ToLower(s); !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

func NewISClient(cfg Config, onLine func(string), onStatus func()) *ISClient {
	login := fmt.Sprintf("user %s pass %s vers %s %s", cfg.Callsign, cfg.Passcode, softwareName, softwareVersion)
	if f := effectiveISFilter(cfg); f != "" {
		login += " filter " + f
	}
	return &ISClient{servers: isServers(cfg), login: login, onLine: onLine, onStatus: onStatus, out: make(chan string, 200)}
}

func (c *ISClient) Status() ISStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

func (c *ISClient) setStatus(f func(*ISStatus)) {
	c.mu.Lock()
	f(&c.st)
	c.mu.Unlock()
	c.onStatus()
}

// Send は接続中であれば 1 行送信キューに入れる。
func (c *ISClient) Send(line string) bool {
	if !c.Status().Connected {
		return false
	}
	select {
	case c.out <- line:
		return true
	default:
		return false
	}
}

// Run は APRS-IS に接続し、切れたら再接続する。接続先は常に主 → 副 → 予備の順に試し、
// つながらなければ次へ進む。つながった接続が切れたら、また主から試し直す (主が復旧していれば戻る)。
// 接続は同時に 1 本だけで、毎回ホスト名から DNS を引き直す。
func (c *ISClient) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		start := time.Now()
		var err error
		for i, srv := range c.servers {
			var connected bool
			connected, err = c.session(ctx, srv)
			if ctx.Err() != nil {
				c.setStatus(func(s *ISStatus) { s.Connected, s.Verified, s.Server = false, false, "" })
				return
			}
			c.setStatus(func(s *ISStatus) {
				s.Connected, s.Verified, s.Server = false, false, ""
				if err != nil {
					s.Error = err.Error()
				}
			})
			if err != nil {
				log.Printf("APRS-IS (%s): %v", srv, err)
			}
			if connected {
				break // いったんつながった接続が切れた: 主から試し直す
			}
			if i+1 < len(c.servers) {
				log.Printf("APRS-IS: %s に接続できないため %s を試します", srv, c.servers[i+1])
			}
		}
		if time.Since(start) > 2*time.Minute {
			backoff = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
}

// session は 1 つのサーバーに接続して読み続ける。つながった (ログインを送った) 場合 connected が true になる。
func (c *ISClient) session(ctx context.Context, server string) (connected bool, _ error) {
	d := net.Dialer{Timeout: 20 * time.Second, KeepAlive: 60 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", server)
	if err != nil {
		return false, err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		conn.Close()
	}()
	log.Printf("APRS-IS: %s に接続しました", conn.RemoteAddr())

	// 未送信キューの残りは破棄
	for len(c.out) > 0 {
		<-c.out
	}
	if _, err := fmt.Fprintf(conn, "%s\r\n", c.login); err != nil {
		return false, err
	}
	c.setStatus(func(s *ISStatus) { s.Connected, s.Error = true, "" })

	// 送信ゴルーチン
	go func() {
		for {
			select {
			case <-done:
				return
			case line := <-c.out:
				conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
				if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
					conn.Close()
					return
				}
			}
		}
	}()

	r := bufio.NewReaderSize(conn, 4096)
	for {
		conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
		line, err := r.ReadString('\n')
		if err != nil {
			if ctx.Err() != nil {
				return connected, nil
			}
			return connected, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		if line[0] == '#' {
			if strings.HasPrefix(line, "# logresp") {
				f := strings.Fields(line)
				verified := len(f) >= 4 && f[3] == "verified,"
				server := ""
				if i := strings.Index(line, "server "); i >= 0 {
					server = strings.TrimSpace(line[i+7:])
				}
				connected = true // サーバーがログインに応答した
				log.Printf("APRS-IS: %s", line)
				c.setStatus(func(s *ISStatus) { s.Verified, s.Server = verified, server })
			}
			continue
		}
		c.onLine(line)
	}
}
