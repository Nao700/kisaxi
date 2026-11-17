package main

import (
	"bufio"
	"context"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestISServers(t *testing.T) {
	c := Config{ISServer: " rotate.aprs2.net:14580 ", ISServer2: "ROTATE.aprs2.net:14580", ISServer3: "asia.aprs2.net"}
	want := []string{"rotate.aprs2.net:14580", "asia.aprs2.net:14580"}
	if got := isServers(c); !reflect.DeepEqual(got, want) {
		t.Errorf("isServers = %v, want %v", got, want)
	}
	if got := isServers(Config{ISServer: "a:1", ISServer2: ""}); len(got) != 1 {
		t.Errorf("空欄は除く: %v", got)
	}
}

// 主がつながらない・応答しないとき、副、予備の順に切り替わる。
func TestISFailover(t *testing.T) {
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := dead.Addr().String()
	dead.Close() // 主: 接続拒否

	mute, _ := net.Listen("tcp", "127.0.0.1:0") // 副: 受け付けるが logresp を返さず閉じる
	defer mute.Close()
	go func() {
		for {
			c, err := mute.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	good, _ := net.Listen("tcp", "127.0.0.1:0") // 予備: 正常
	defer good.Close()
	got := make(chan string, 1)
	go func() {
		c, err := good.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		c.Write([]byte("# logresp T verified, server TEST\r\nA>B:hello\r\n"))
		got <- line
		time.Sleep(time.Second)
	}()

	lines := make(chan string, 1)
	cfg := Config{Callsign: "T", Passcode: "-1", ISServer: deadAddr, ISServer2: mute.Addr().String(), ISServer3: good.Addr().String()}
	c := NewISClient(cfg, func(l string) { lines <- l }, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	select {
	case l := <-lines:
		if l != "A>B:hello" {
			t.Errorf("line = %q", l)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("予備サーバーに切り替わらなかった")
	}
	<-got
}
