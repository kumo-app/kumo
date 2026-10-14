package tun

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"kumo/corepin"
)

func TestShellQuote(t *testing.T) {
	if got := shellQuote(`/a b/it's`); got != `'/a b/it'\''s'` {
		t.Fatal(got)
	}
}

func TestHashCaseListsPins(t *testing.T) {
	for _, h := range corepin.Trusted(runtime.GOOS) {
		if !strings.Contains(hashCase(), h) {
			t.Fatalf("%s missing from %s", h, hashCase())
		}
	}
}

func TestDigestCache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "core")
	os.WriteFile(p, []byte("one"), 0o644)
	a, err := cachedDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("two!"), 0o644) // another size: another key
	b, _ := cachedDigest(p)
	if a == b {
		t.Fatal("a changed file kept its digest")
	}
	if trusted(p) {
		t.Fatal("an unknown file is trusted")
	}
}

func TestInstallRefusesUntrusted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mihomo")
	os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	if err := Install(p); err != ErrUntrusted {
		t.Fatalf("Install = %v, want ErrUntrusted", err)
	}
}
