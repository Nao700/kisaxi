package main

import (
	"reflect"
	"testing"
	"time"
)

func TestRestartArgsNoDuplicates(t *testing.T) {
	first := restartArgs([]string{"-listen", "0.0.0.0:8073"})
	second := restartArgs(first)
	want := []string{"-after-update", "-nobrowser", "-listen", "0.0.0.0:8073"}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Fatalf("first=%v second=%v want=%v", first, second, want)
	}
}

func TestRetryWait(t *testing.T) {
	fails := 0
	for i := 0; i < 5; i++ {
		if w := retryWait(&fails, false); w != 2*time.Second {
			t.Fatalf("fail %d: %v", i+1, w)
		}
	}
	if w := retryWait(&fails, false); w != 10*time.Second {
		t.Fatalf("6th failure: %v", w)
	}
	if w := retryWait(&fails, true); w != 2*time.Second || fails != 0 {
		t.Fatalf("after connected: %v fails=%d", w, fails)
	}
}
