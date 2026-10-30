package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRtl433DecoderFineOffset(t *testing.T) {
	d := newRtl433Decoder("", "")
	now := time.Now()
	w, ok := d.feed([]byte(`{"time":"2026-10-30 10:00:00","model":"Fineoffset-WH1080","id":42,"temperature_C":21.5,"humidity":55,"wind_avg_km_h":18,"wind_max_km_h":30,"wind_dir_deg":225,"rain_mm":120.3}`), now)
	if !ok {
		t.Fatal("気象データとして読めない")
	}
	if !near(*w.Temp, 21.5) || *w.Humidity != 55 || !near(*w.Wind, 5) || *w.WindDir != 225 || !near(*w.RainMidnight, 120.3) {
		t.Errorf("%+v", w)
	}
	if w.Pressure != nil || w.Luminosity != nil {
		t.Errorf("無い項目が入っている: %+v", w)
	}
}

func TestRtl433DecoderUnitsAndMerge(t *testing.T) {
	d := newRtl433Decoder("", "")
	t0 := time.Now()
	// 温湿度だけのメッセージ → 風・雨は後から来る別メッセージで補う
	if w, ok := d.feed([]byte(`{"model":"Acurite-Tower","temperature_F":68,"humidity":40}`), t0); !ok || !near(*w.Temp, 20) {
		t.Fatalf("temp %+v ok=%v", w, ok)
	}
	w, ok := d.feed([]byte(`{"model":"Acurite-5n1","wind_avg_mi_h":10,"wind_dir_deg":90,"rain_in":2,"pressure_kPa":101.3,"light_lux":12670}`), t0.Add(time.Minute))
	if !ok {
		t.Fatal("2 件目が読めない")
	}
	if !near(*w.Temp, 20) || *w.Humidity != 40 { // 1 件目の値が残る
		t.Errorf("温湿度が引き継がれない: %+v", w)
	}
	if !near(*w.Wind, 10*mphToMs) || !near(*w.RainMidnight, 50.8) || !near(*w.Pressure, 1013) || *w.Luminosity != 100 {
		t.Errorf("%+v", w)
	}
	// 古い値は組み合わせない
	w, _ = d.feed([]byte(`{"model":"x","wind_avg_m_s":3}`), t0.Add(2*time.Hour))
	if w.Temp != nil || w.Humidity != nil || !near(*w.Wind, 3) {
		t.Errorf("古い値が残っている: %+v", w)
	}
}

func TestRtl433DecoderFilterAndGarbage(t *testing.T) {
	d := newRtl433Decoder("wh1080", "42")
	now := time.Now()
	for name, line := range map[string]string{
		"別のモデル":   `{"model":"Acurite-5n1","id":42,"temperature_C":10}`,
		"別の id":   `{"model":"Fineoffset-WH1080","id":7,"temperature_C":10}`,
		"JSON 以外": `rtl_433: tuned to 433.920MHz`,
		"気象の項目なし": `{"model":"Fineoffset-WH1080","id":42,"battery_ok":1}`,
		"範囲外の値":   `{"model":"Fineoffset-WH1080","id":42}`,
	} {
		if w, ok := d.feed([]byte(line), now); ok {
			t.Errorf("%s: 受け付けた %+v", name, w)
		}
	}
	if w, ok := d.feed([]byte(`{"model":"Fineoffset-WH1080","id":42,"temperature_C":150,"humidity":300}`), now); !ok || w.Temp != nil || w.Humidity != nil {
		t.Errorf("範囲外の温湿度を採用した: %+v", w)
	}
	if _, ok := d.feed([]byte(`{"model":"Fineoffset-WH1080","id":42,"temperature_C":9}`), now); !ok {
		t.Error("一致するデータを受け付けない")
	}
}

func TestRtl433ClientRunsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh が必要")
	}
	script := filepath.Join(t.TempDir(), "fake_rtl_433")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'tuned' >&2\necho '{\"model\":\"m\",\"temperature_C\":12.5,\"humidity\":60}'\nsleep 30\n"), 0o755)
	cfg := defaultConfig()
	cfg.WXRtlCmd = script + " -F json"

	got := make(chan *Weather, 2)
	c := NewRtl433Client(cfg, func(w *Weather, _ time.Time) { got <- w }, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	select {
	case w := <-got:
		if !near(*w.Temp, 12.5) || *w.Humidity != 60 {
			t.Errorf("%+v", w)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rtl_433 の出力を読めない")
	}
	if st := c.Status(); !st.Connected || st.Last == 0 {
		t.Errorf("status %+v", st)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("停止しない")
	}
}

func TestRtl433ClientMissingBinary(t *testing.T) {
	cfg := defaultConfig()
	cfg.WXRtlCmd = "/nonexistent/rtl_433"
	c := NewRtl433Client(cfg, func(*Weather, time.Time) {}, func() {})
	err := c.session(context.Background())
	if err == nil || c.Status().Last != 0 {
		t.Errorf("err=%v", err)
	}
}
