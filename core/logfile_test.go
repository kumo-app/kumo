package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCapFileRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	os.WriteFile(path, []byte("from the last run\n"), 0o600)
	c, err := openCapped(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	line := bytes.Repeat([]byte("x"), 29)
	line = append(line, '\n')
	for range 10 {
		if _, err := c.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	c.Close()
	cur, _ := os.ReadFile(path)
	prev, _ := os.ReadFile(path + ".1")
	if len(cur) > 100 || len(prev) > 100 || len(cur) == 0 || len(prev) == 0 {
		t.Fatalf("sizes %d and %d", len(cur), len(prev))
	}
	if len(cur)%30 != 0 || len(prev)%30 != 0 {
		t.Fatal("a line was split")
	}
	if bytes.Contains(cur, []byte("last run")) {
		t.Fatal("the last run's log was kept")
	}
	if n, _ := c.Write(line); n != len(line) {
		t.Fatal("a closed log must swallow writes")
	}
}
