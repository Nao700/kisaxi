package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBTBridge(t *testing.T) {
	kiss, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer kiss.Close()
	go func() {
		c, err := kiss.Accept()
		if err == nil {
			c.Write([]byte{0xC0})
			c.Close()
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/devices", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"name":"TH-D75","addr":"AA:BB:CC:DD:EE:FF"}]`))
	})
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Query().Get("addr") != "AA:BB:CC:DD:EE:FF" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"bad"}`))
			return
		}
		w.Write([]byte(`{"ok":true,"kiss":"` + kiss.Addr().String() + `"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	addr := srv.Listener.Addr().String()

	devs, err := listBTDevices(context.Background(), addr)
	if err != nil || len(devs) != 1 || devs[0].Name != "TH-D75" {
		t.Fatalf("devs=%v err=%v", devs, err)
	}
	rw, err := openBT(context.Background(), Config{BTAddr: "AA:BB:CC:DD:EE:FF", BTBridgeAddr: addr})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if n, _ := rw.Read(buf); n != 1 || buf[0] != 0xC0 {
		t.Fatal("kiss relay not reached")
	}
	rw.Close()
	if _, err := openBT(context.Background(), Config{}); err == nil {
		t.Fatal("expected error for empty addr")
	}
	if _, err := listBTDevices(context.Background(), "127.0.0.1:1"); err == nil {
		t.Fatal("expected error when bridge is down")
	}
}
