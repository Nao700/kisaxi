package main

import "testing"

func TestIsDPRS(t *testing.T) {
	cases := map[string]bool{
		"JA1ABC>APDPRS,DSTAR*,qAR,JA1RL:!3540.00N/13946.00E>": true,
		"JA1ABC>APDG03,TCPIP*:!3540.00N/13946.00E>":           true,
		"JA1ABC>API510,DSTAR*:!3540.00N/13946.00E>":           true,
		"JA1ABC>APD5T1:!3540.00N/13946.00E>":                  true,
		"JA1ABC>APJID2:!3540.00N/13946.00E>":                  true,
		"JA1ABC>AP123D:!3540.00N/13946.00E>":                  true,
		"JA1ABC>APRS,WIDE1-1:!3540.00ND13946.00E&":            false, // オーバーレイ D の別機器
		"JA1ABC>APY02D,WIDE1-1:!3540.00N/13946.00E>":          false,
		"JA1ABC>APDW17,WIDE1-1:!3540.00N/13946.00E>":          false, // DireWolf
	}
	for line, want := range cases {
		p, err := ParseTNC2(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if got := isDPRS(p); got != want {
			t.Errorf("%s: got %v want %v", line, got, want)
		}
	}
	p, _ := ParseTNC2("JA1ABC>APDPRS:!3540.00N/13946.00E>")
	if !ignoredPacket(Config{NoDPRS: true}, p) || ignoredPacket(Config{}, p) {
		t.Error("NoDPRS の切り替えが効いていない")
	}
}
