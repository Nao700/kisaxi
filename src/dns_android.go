//go:build android

package main

// Android には /etc/resolv.conf がなく、Go の標準リゾルバは 127.0.0.1:53 / [::1]:53 に問い合わせて失敗する
// (APRS-IS のホスト名が引けず、接続できないままになる)。そのため DNS サーバーを明示する。
// Android ラッパーアプリが端末の DNS を環境変数 KISAXI_DNS (カンマ区切り) で渡す。無ければ公開 DNS を使う。

import (
	"context"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func init() {
	var servers []string
	for _, s := range strings.Split(os.Getenv("KISAXI_DNS"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			servers = append(servers, s)
		}
	}
	servers = append(servers, "1.1.1.1", "8.8.8.8")
	var n atomic.Uint32 // 呼ばれるたびに次のサーバーへ回し、応答しないサーバーで止まらないようにする
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			var last error
			start := int(n.Add(1)) % len(servers)
			for i := range servers {
				s := servers[(start+i)%len(servers)]
				c, err := d.DialContext(ctx, network, net.JoinHostPort(s, "53"))
				if err == nil {
					return c, nil
				}
				last = err
			}
			return nil, last
		},
	}
}
