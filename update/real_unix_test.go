//go:build !windows

package update

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestApplyRealProcess swaps a tar install and starts the new version as
// a real process, which reports in through OKEnv.
func TestApplyRealProcess(t *testing.T) {
	in := installed(t, "old")
	script := "#!/bin/sh\n[ -n \"$KUMO_UPDATE_OK\" ] && echo ok > \"$KUMO_UPDATE_OK\"\n"
	p, err := Prepare(in, makeTar(t, map[string]string{"kumo": script}), "0.5.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.PID = 0x7ffffff0 // no such process: Kumo has quit
	p.Wait = 10 * time.Second
	r := apply(p, log.New(io.Discard, "", 0))
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(in.Target, "kumo")); string(b) != script {
		t.Fatal("not swapped")
	}
}
