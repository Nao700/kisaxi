package main

import "testing"

func TestEffectiveISFilter(t *testing.T) {
	c := Config{Passcode: "-1", Lat: 33.59, Lon: 130.4, ISFilter: "m/150 p/JE6UER"}
	if got := effectiveISFilter(c); got != "r/33.5900/130.4000/150 p/JE6UER" {
		t.Fatal(got)
	}
	c.Passcode = "12345" // 送信できる局は m/ のまま
	if got := effectiveISFilter(c); got != "m/150 p/JE6UER" {
		t.Fatal(got)
	}
	c = Config{Passcode: "-1", ISFilter: "m/50"} // 位置未設定
	if got := effectiveISFilter(c); got != "m/50" {
		t.Fatal(got)
	}
	c = Config{Passcode: "-1", Lat: 1, Lon: 2, ISFilter: "r/1/2/10 b/JA1*"}
	if got := effectiveISFilter(c); got != "r/1/2/10 b/JA1*" {
		t.Fatal(got)
	}
}
