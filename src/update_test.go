package main

import (
	"context"
	"net/http"
	"net/http/httptest"
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
				"assets":[{"name":"aprsigate-linux-x64","browser_download_url":"https://example.com/x64"},
				{"name":"aprsigate-windows-x64.exe","browser_download_url":"https://example.com/exe"}]}`))
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

	u := fetchUpdate(ctx, srv.URL+"/new", "1.0.5", "aprsigate-windows-x64.exe")
	if !u.Newer || u.Latest != "1.0.6" || u.URL != "https://example.com/rel" || u.Asset != "https://example.com/exe" || u.Error != "" {
		t.Errorf("new: %+v", u)
	}
	u = fetchUpdate(ctx, srv.URL+"/same", "1.0.5", "aprsigate-linux-x64")
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
	if a := platformAsset(); a == "" || a[:10] != "aprsigate-" {
		t.Errorf("platformAsset() = %q", a)
	}
}
