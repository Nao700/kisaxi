package main

// 日別パケットログ (logs/YYYYMMDD.log)

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type PacketLogger struct {
	mu      sync.Mutex
	dir     string
	enabled bool
	day     string
	f       *os.File
}

func NewPacketLogger(dir string) *PacketLogger { return &PacketLogger{dir: dir} }

func (l *PacketLogger) SetEnabled(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = on
	if !on && l.f != nil {
		l.f.Close()
		l.f, l.day = nil, ""
	}
}

func (l *PacketLogger) Write(dir, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return
	}
	now := time.Now()
	day := now.Format("20060102")
	if day != l.day || l.f == nil {
		if l.f != nil {
			l.f.Close()
			l.f = nil
		}
		if err := os.MkdirAll(l.dir, 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(l.dir, day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		l.f, l.day = f, day
	}
	fmt.Fprintf(l.f, "%s [%s] %s\n", now.Format("2006-01-02 15:04:05"), dir, line)
}
