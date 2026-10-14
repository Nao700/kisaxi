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
	server, login string
	onLine        func(string)
	onStatus      func()

	out chan string
	mu  sync.Mutex
	st  ISStatus
}

func NewISClient(cfg Config, onLine func(string), onStatus func()) *ISClient {
	login := fmt.Sprintf("user %s pass %s vers %s %s", cfg.Callsign, cfg.Passcode, softwareName, softwareVersion)
	if f := strings.TrimSpace(cfg.ISFilter); f != "" {
		login += " filter " + f
	}
	return &ISClient{server: cfg.ISServer, login: login, onLine: onLine, onStatus: onStatus, out: make(chan string, 200)}
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

func (c *ISClient) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		start := time.Now()
		err := c.session(ctx)
		c.setStatus(func(s *ISStatus) {
			s.Connected, s.Verified, s.Server = false, false, ""
			if err != nil {
				s.Error = err.Error()
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("APRS-IS: %v", err)
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

func (c *ISClient) session(ctx context.Context) error {
	d := net.Dialer{Timeout: 20 * time.Second, KeepAlive: 60 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.server)
	if err != nil {
		return err
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
		return err
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
				return nil
			}
			return err
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
				log.Printf("APRS-IS: %s", line)
				c.setStatus(func(s *ISStatus) { s.Verified, s.Server = verified, server })
			}
			continue
		}
		c.onLine(line)
	}
}
