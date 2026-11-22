package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.5", "1.0.5", 0},
		{"v1.0.6", "1.0.5", 1},
		{"1.0.10", "1.0.9", 1},
		{"1.1", "1.0.9", 1},
		{"1.0", "1.0.0", 0},
		{"1.0.4", "1.0.5", -1},
		{"V2.0.0-beta", "1.9.9", 1},
		{"", "1.0.5", -1},
	}
	for _, c := range cases {
		if got := compareVersion(c.a, c.b); got != c.want {
			t.Errorf("compareVersion(%q, %q) = %d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestFetchUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/new":
			w.Write([]byte(`{"tag_name":"v1.0.6","html_url":"https://example.com/rel",
				"assets":[{"name":"KISAXI-linux-x64","browser_download_url":"https://example.com/x64"},
				{"name":"KISAXI-windows-x64.exe","browser_download_url":"https://example.com/exe"}]}`))
		case "/same":
			w.Write([]byte(`{"tag_name":"1.0.5","html_url":"https://example.com/rel","assets":[]}`))
		case "/broken":
			w.Write([]byte(`not json`))
		case "/error":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	u := fetchUpdate(ctx, srv.URL+"/new", "1.0.5", "KISAXI-windows-x64.exe")
	if !u.Newer || u.Latest != "1.0.6" || u.URL != "https://example.com/rel" || u.Asset != "https://example.com/exe" || u.Error != "" {
		t.Errorf("new: %+v", u)
	}
	u = fetchUpdate(ctx, srv.URL+"/same", "1.0.5", "KISAXI-linux-x64")
	if u.Newer || u.Latest != "1.0.5" || u.Asset != "" || u.Error != "" {
		t.Errorf("same: %+v", u)
	}
	u = fetchUpdate(ctx, srv.URL+"/none", "1.0.5", "")
	if u.Newer || u.Latest != "" || u.Error != "" {
		t.Errorf("no release: %+v", u)
	}
	for _, p := range []string{"/broken", "/error"} {
		if u = fetchUpdate(ctx, srv.URL+p, "1.0.5", ""); u.Newer || u.Error == "" {
			t.Errorf("%s: %+v", p, u)
		}
	}
}

func TestUpdateCheckOffByDefault(t *testing.T) {
	if defaultConfig().UpdateCheck {
		t.Error("更新の自動確認は既定でオフにする")
	}
}

func TestPlatformAsset(t *testing.T) {
	if a := platformAsset(); a == "" || a[:7] != "KISAXI-" {
		t.Errorf("platformAsset() = %q", a)
	}
}

func fakeBinary(size int) []byte {
	b := make([]byte, size)
	copy(b, []byte("\x7fELF"))
	if runtime.GOOS == "windows" {
		copy(b, []byte("MZ"))
	}
	return b
}

func TestDownloadBinary(t *testing.T) {
	good := fakeBinary(minBinarySize + 10)
	sum := sha256.Sum256(good)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			w.Write(good)
		case "/small":
			w.Write(fakeBinary(100))
		case "/html":
			w.Write(bytes.Repeat([]byte("<html>"), minBinarySize))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx := context.Background()

	p, err := downloadBinary(ctx, srv.URL+"/good", "sha256:"+hex.EncodeToString(sum[:]), dir)
	if err != nil {
		t.Fatalf("good: %v", err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, good) {
		t.Error("downloaded content differs")
	}
	if _, err := downloadBinary(ctx, srv.URL+"/good", "sha256:"+strings.Repeat("0", 64), dir); err == nil {
		t.Error("checksum mismatch accepted")
	}
	for _, path := range []string{"/small", "/html", "/missing"} {
		if _, err := downloadBinary(ctx, srv.URL+path, "", dir); err == nil {
			t.Errorf("%s accepted", path)
		}
	}
	if _, err := downloadBinary(ctx, "http://example.com/x", "", dir); err == nil {
		t.Error("plain http accepted")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("temp files left behind: %d entries", len(ents))
	}
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	exe, nw := filepath.Join(dir, "app"), filepath.Join(dir, ".update-1")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(nw, []byte("new"), 0o755)
	if err := replaceExecutable(exe, nw); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Errorf("exe = %q", got)
	}
}
