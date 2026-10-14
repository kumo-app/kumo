package corepin

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestMatchesLock(t *testing.T) {
	data, err := os.ReadFile("../scripts/fetch-core/core.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lk struct {
		Assets   map[string]struct{ Binary string } `json:"assets"`
		Packaged map[string]string                  `json:"packaged"`
	}
	if err := json.Unmarshal(data, &lk); err != nil {
		t.Fatal(err)
	}
	if len(lk.Assets) != len(Binaries) || len(lk.Packaged) != len(Packaged) {
		t.Fatalf("pins differ from core.lock.json")
	}
	for k, a := range lk.Assets {
		if Binaries[k] != a.Binary {
			t.Errorf("%s: corepin %q, lock %q", k, Binaries[k], a.Binary)
		}
	}
	for k, v := range lk.Packaged {
		if Packaged[k] != v {
			t.Errorf("packaged %s: corepin %q, lock %q", k, Packaged[k], v)
		}
	}
}

func TestTrusted(t *testing.T) {
	if l := Trusted("linux"); len(l) != 2 || !slices.Contains(l, Binaries["linux-amd64"]) {
		t.Fatalf("linux: %v", l)
	}
	if l := Trusted("darwin"); !slices.Contains(l, Binaries["darwin-arm64"]) {
		t.Fatalf("darwin: %v", l)
	}
	if len(Trusted("plan9")) != 0 {
		t.Fatal("plan9 trusted")
	}
}
