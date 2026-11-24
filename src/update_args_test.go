package main

import (
	"reflect"
	"testing"
)

func TestRestartArgsNoDuplicates(t *testing.T) {
	first := restartArgs([]string{"-listen", "0.0.0.0:8073"})
	second := restartArgs(first)
	want := []string{"-after-update", "-nobrowser", "-listen", "0.0.0.0:8073"}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Fatalf("first=%v second=%v want=%v", first, second, want)
	}
}
