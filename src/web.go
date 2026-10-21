package main

// ブラウザ UI 用 HTTP サーバー (REST + Server-Sent Events)

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }

func (h *Hub) Broadcast(typ string, v any) {
	b, err := json.Marshal(map[string]any{"type": typ, "data": v})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- b:
		default: // 遅いクライアントは取りこぼしを許容
		}
	}
}

func (h *Hub) subscribe() chan []byte {
	c := make(chan []byte, 512)
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *Hub) unsubscribe(c chan []byte) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// sameOrigin は他サイトからの POST (CSRF) を拒否するための確認。
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

func (a *App) Handler(static fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(static)))

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"config":   a.Config(),
			"status":   a.Status(),
			"stations": a.db.All(),
			"messages": a.msgs.All(),
			"packets":  a.recent.All(),
		})
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		c := a.hub.subscribe()
		defer a.hub.unsubscribe(c)
		fmt.Fprint(w, ": hello\n\n")
		fl.Flush()
		keep := time.NewTicker(15 * time.Second)
		defer keep.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-c:
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			case <-keep.C:
				fmt.Fprint(w, ": ping\n\n")
				fl.Flush()
			}
		}
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, a.Config())
		case http.MethodPost:
			if !sameOrigin(r) {
				writeErr(w, http.StatusForbidden, "forbidden")
				return
			}
			cfg := a.Config()
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&cfg); err != nil {
				writeErr(w, http.StatusBadRequest, "設定データが不正です")
				return
			}
			if err := a.ApplyConfig(cfg); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, a.Config())
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		var req struct{ To, Text string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "不正なリクエスト")
			return
		}
		m, err := a.SendMessage(req.To, req.Text)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, m)
	})

	mux.HandleFunc("/api/beacon", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		rf, is := a.BeaconNow()
		writeJSON(w, map[string]bool{"rf": rf, "is": is})
	})

	// オブジェクト一覧の保存 (他の設定は変えない)
	mux.HandleFunc("/api/objects", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		var list []ObjectDef
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&list); err != nil {
			writeErr(w, http.StatusBadRequest, "オブジェクトのデータが不正です")
			return
		}
		cfg := a.Config()
		cfg.Objects = list
		if err := a.ApplyConfig(cfg); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, a.Config())
	})

	mux.HandleFunc("/api/object/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		var req struct{ ID string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "不正なリクエスト")
			return
		}
		if err := a.SendObjectNow(req.ID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("/api/about", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"name":      softwareName,
			"version":   softwareVersion,
			"credit":    developedBy,
			"changelog": changelog,
		})
	})

	// 更新の今すぐ確認 (設定で自動確認がオフでも使える)
	mux.HandleFunc("/api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		writeJSON(w, a.CheckUpdate(r.Context()))
	})

	mux.HandleFunc("/api/serialports", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, listSerialPorts())
	})
	return mux
}
