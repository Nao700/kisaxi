package main

// rtl_433 (https://github.com/merbanan/rtl_433) の JSON 出力から気象データを読み取る (β版)。
// rtl_433 を子プロセスとして起動し、標準出力の JSON (1 行 1 件) を受け取る。
// 市販の 433MHz 帯の簡易気象センサー (Fine Offset / Acurite / Bresser など) を RTL-SDR で受信する用途。

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	rtl433StaleAfter = 30 * time.Minute // これより古いセンサー値は組み合わせない
	rtl433MaxLine    = 1 << 20
)

// wxClient は気象データの読み取り元 (Davis、rtl_433) に共通の操作。
type wxClient interface {
	Run(ctx context.Context)
	Status() DavisStatus
}

// rtlField は組み合わせ中の 1 項目。
type rtlField struct {
	v float64
	t time.Time
}

// rtl433Decoder はセンサーごとに別々に届く JSON を 1 つの Weather にまとめる。
// 同じ機器でも温湿度と風・雨が別メッセージで来ることがあるため、項目ごとに最新値を保持する。
type rtl433Decoder struct {
	model, id string // 空でなければ一致するものだけ使う (model は部分一致、大文字小文字を区別しない)
	f         map[string]rtlField
	heard     []string // 受信した気象センサー ("model id=…")、新しい順。絞り込み前のもの
}

// 気象の項目とみなす JSON のキー (受信できているセンサーの一覧づくりに使う)
var rtl433WXKeys = []string{"temperature_C", "temperature_F", "humidity", "wind_avg_km_h", "wind_avg_m_s", "wind_avg_mi_h",
	"wind_speed_km_h", "wind_speed_m_s", "wind_speed_mi_h", "wind_dir_deg", "rain_mm", "rain_in", "pressure_hPa", "pressure_kPa", "light_lux"}

func (d *rtl433Decoder) noteHeard(m map[string]any) {
	model := jsonText(m, "model")
	if model == "" {
		return
	}
	has := false
	for _, k := range rtl433WXKeys {
		if _, ok := m[k]; ok {
			has = true
			break
		}
	}
	if !has {
		return
	}
	name := model
	if id := jsonText(m, "id"); id != "" {
		name += " id=" + id
	}
	for i, h := range d.heard {
		if h == name {
			d.heard = append(d.heard[:i], d.heard[i+1:]...)
			break
		}
	}
	d.heard = append([]string{name}, d.heard...)
	if len(d.heard) > 8 {
		d.heard = d.heard[:8]
	}
}

func newRtl433Decoder(model, id string) *rtl433Decoder {
	return &rtl433Decoder{model: strings.ToLower(strings.TrimSpace(model)), id: strings.TrimSpace(id), f: map[string]rtlField{}}
}

func jsonNum(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v, true
		}
	}
	return 0, false
}

func jsonText(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%g", v)
	}
	return ""
}

// first は keys の中で最初に見つかった数値を、変換 conv を通して返す。
func first(m map[string]any, conv func(float64) float64, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := jsonNum(m, k); ok {
			return conv(v), true
		}
	}
	return 0, false
}

func ident(v float64) float64 { return v }

// feed は 1 行の JSON を取り込み、気象の項目を含んでいれば現時点でまとめた Weather を返す。
func (d *rtl433Decoder) feed(line []byte, now time.Time) (*Weather, bool) {
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, false
	}
	d.noteHeard(m)
	if d.model != "" && !strings.Contains(strings.ToLower(jsonText(m, "model")), d.model) {
		return nil, false
	}
	if d.id != "" && jsonText(m, "id") != d.id {
		return nil, false
	}
	got := false
	put := func(name string, fn func() (float64, bool)) {
		if v, ok := fn(); ok {
			d.f[name] = rtlField{v, now}
			got = true
		}
	}
	kmh := func(v float64) float64 { return v / 3.6 }
	mph := func(v float64) float64 { return v * mphToMs }
	put("temp", func() (float64, bool) {
		if v, ok := first(m, ident, "temperature_C"); ok {
			return v, true
		}
		return first(m, func(f float64) float64 { return (f - 32) * 5 / 9 }, "temperature_F")
	})
	put("hum", func() (float64, bool) { return first(m, ident, "humidity") })
	put("wind", func() (float64, bool) {
		if v, ok := first(m, ident, "wind_avg_m_s", "wind_speed_m_s", "wind_avg_ms"); ok {
			return v, true
		}
		if v, ok := first(m, kmh, "wind_avg_km_h", "wind_speed_km_h"); ok {
			return v, true
		}
		return first(m, mph, "wind_avg_mi_h", "wind_speed_mi_h", "wind_avg_mph")
	})
	put("dir", func() (float64, bool) { return first(m, ident, "wind_dir_deg", "wind_direction_deg") })
	put("rain", func() (float64, bool) {
		if v, ok := first(m, ident, "rain_mm"); ok {
			return v, true
		}
		return first(m, func(i float64) float64 { return i * 25.4 }, "rain_in")
	})
	put("pres", func() (float64, bool) {
		if v, ok := first(m, ident, "pressure_hPa"); ok {
			return v, true
		}
		return first(m, func(k float64) float64 { return k * 10 }, "pressure_kPa")
	})
	// 照度 (lux) は晴天の太陽光でおよそ 126.7 lux ≒ 1 W/m² として換算する
	put("lux", func() (float64, bool) { return first(m, func(l float64) float64 { return l / 126.7 }, "light_lux") })
	if !got {
		return nil, false
	}

	w := &Weather{}
	cur := func(name string) (float64, bool) {
		f, ok := d.f[name]
		if !ok || now.Sub(f.t) > rtl433StaleAfter {
			return 0, false
		}
		return f.v, true
	}
	if v, ok := cur("temp"); ok && v > -90 && v < 70 {
		w.Temp = fptr(v)
	}
	if v, ok := cur("hum"); ok && v >= 0 && v <= 100 {
		w.Humidity = iptr(int(math.Round(v)))
	}
	if v, ok := cur("wind"); ok && v >= 0 && v < 100 {
		w.Wind = fptr(v)
	}
	if v, ok := cur("dir"); ok && v >= 0 && v <= 360 {
		w.WindDir = iptr(int(math.Round(v)) % 360)
	}
	if v, ok := cur("pres"); ok && v > 500 && v < 1200 {
		w.Pressure = fptr(v)
	}
	if v, ok := cur("lux"); ok && v >= 0 {
		w.Luminosity = iptr(int(math.Round(v)))
	}
	// rain_mm は電源投入からの積算値。wxIngest は RainMidnight の増分を雨量にするので、そのまま渡す
	// (値が減ったとき (センサーの電池交換など) は、新しい値を増分として扱う)。
	if v, ok := cur("rain"); ok && v >= 0 {
		w.RainMidnight = fptr(v)
	}
	return w, true
}

// Rtl433Client は rtl_433 を起動して読み取った気象データを onReading に渡す。
type Rtl433Client struct {
	cmd       []string
	dec       *rtl433Decoder
	onReading func(*Weather, time.Time)
	onStatus  func()

	mu sync.Mutex
	st DavisStatus
}

// splitCommand は空白区切りでコマンドを分ける (シェルは使わない)。引用符は扱わない。
func splitCommand(s string) []string { return strings.Fields(s) }

func NewRtl433Client(cfg Config, onReading func(*Weather, time.Time), onStatus func()) *Rtl433Client {
	cmd := splitCommand(cfg.WXRtlCmd)
	if len(cmd) == 0 {
		cmd = splitCommand(defaultConfig().WXRtlCmd)
	}
	return &Rtl433Client{
		cmd: cmd, dec: newRtl433Decoder(cfg.WXRtlModel, cfg.WXRtlID),
		onReading: onReading, onStatus: onStatus,
		st: DavisStatus{Desc: "rtl_433 (β版)"},
	}
}

func (c *Rtl433Client) Status() DavisStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

func (c *Rtl433Client) setStatus(connected bool, errText string) {
	c.mu.Lock()
	c.st.Connected, c.st.Error = connected, errText
	c.mu.Unlock()
	c.onStatus()
}

func (c *Rtl433Client) Run(ctx context.Context) {
	for {
		err := c.session(ctx)
		c.setStatus(false, errString(err))
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("気象ステーション (rtl_433): %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (c *Rtl433Client) session(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, c.cmd[0], c.cmd[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%s が見つかりません (rtl_433 をインストールするか、設定でパスを指定してください)", c.cmd[0])
		}
		return err
	}
	log.Printf("気象ステーション (rtl_433): 起動しました (%s)", strings.Join(c.cmd, " "))
	c.setStatus(true, "")

	// rtl_433 は状況を標準エラーに出す。直近の行を、異常終了したときの理由として残す
	var lastErr string
	var errMu sync.Mutex
	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if s := strings.TrimSpace(sc.Text()); s != "" {
				errMu.Lock()
				lastErr = s
				errMu.Unlock()
			}
		}
	}()

	// 停止するときは、孫プロセスが標準出力を握っていても読み取りが止まらないように、こちらから閉じる
	readDone := make(chan struct{})
	defer close(readDone)
	go func() {
		select {
		case <-ctx.Done():
			stdout.Close()
		case <-readDone:
		}
	}()

	r := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := readLine(r, rtl433MaxLine)
		if len(line) > 0 {
			now := time.Now()
			w, ok := c.dec.feed(line, now)
			c.mu.Lock()
			c.st.Heard = append(c.st.Heard[:0:0], c.dec.heard...)
			if ok {
				c.st.Last, c.st.Error = now.UnixMilli(), ""
			}
			c.mu.Unlock()
			if ok {
				c.onReading(w, now)
			}
		}
		if err != nil {
			break
		}
	}
	waitErr := cmd.Wait()
	<-errDone
	if ctx.Err() != nil {
		return nil
	}
	errMu.Lock()
	defer errMu.Unlock()
	switch {
	case lastErr != "":
		return fmt.Errorf("rtl_433 が終了しました: %s", lastErr)
	case waitErr != nil:
		return fmt.Errorf("rtl_433 が終了しました: %v", waitErr)
	}
	return errors.New("rtl_433 が終了しました")
}

// readLine は 1 行を読む。max バイトを超える行は捨てる。
func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		part, isPrefix, err := r.ReadLine()
		if len(buf)+len(part) <= max {
			buf = append(buf, part...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(buf) > 0 {
				return buf, nil
			}
			return nil, err
		}
		if !isPrefix {
			return buf, nil
		}
	}
}
