package main

import "errors"

// openRTLSDR は RTL-SDR を開く。USB の直接制御はプラットフォームごとの実装で提供する。
func openRTLSDR(cfg Config) (iqStream, string, error) {
	return nil, "", errors.New("RTL-SDR の直接制御はまだ実装されていません")
}
