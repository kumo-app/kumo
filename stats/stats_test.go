package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kumo/clash"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func conn(id, app string, up, down int64, start time.Time, chain ...string) clash.Connection {
	var c clash.Connection
	c.ID, c.Upload, c.Download, c.Start, c.Chains = id, up, down, start, chain
	c.Metadata.Process = app
	return c
}

func snap(up, down int64, cs ...clash.Connection) clash.Connections {
	return clash.Connections{UploadTotal: up, DownloadTotal: down, Connections: cs}
}

func open(t *testing.T, path string, c *clock) *Store {
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = c.now
	return s
}

func TestDeltasClosesAndRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	c := &clock{time.Date(2026, 10, 14, 10, 0, 0, 0, time.Local)}
	s := open(t, path, c)
	s.NewCore(true)
	t0 := c.t.Add(-time.Second)
	s.Record(snap(10, 100, conn("a", "curl", 10, 100, t0, "HK 1", "Auto", "Proxy")))
	c.t = c.t.Add(time.Second)
	// a grows; b opens; then a closes having sent 5 more bytes unseen.
	s.Record(snap(30, 300, conn("a", "curl", 20, 250, t0, "HK 1", "Auto", "Proxy"), conn("b", "Safari", 0, 40, c.t, "DIRECT")))
	c.t = c.t.Add(time.Second)
	s.Record(snap(35, 340, conn("b", "Safari", 0, 40, c.t, "DIRECT")))
	d := s.Days(1)[0]
	if d.Total != (Bytes{35, 340}) {
		t.Fatalf("total %+v", d.Total)
	}
	if d.Apps["curl"] != (Bytes{20, 250}) || d.Apps["Safari"] != (Bytes{0, 40}) || d.Apps[Other] != (Bytes{15, 50}) {
		t.Fatalf("apps %+v", d.Apps)
	}
	if d.Nodes["HK 1"] != (Bytes{20, 250}) || d.Groups["Auto"] != (Bytes{20, 250}) || d.Groups["Proxy"] != (Bytes{20, 250}) || d.Nodes["DIRECT"] != (Bytes{0, 40}) {
		t.Fatalf("nodes %+v groups %+v", d.Nodes, d.Groups)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Kumo restarts; the external core keeps running with b, which grew.
	c.t = c.t.Add(time.Minute)
	s = open(t, path, c)
	s.NewCore(false)
	s.Record(snap(35, 400, conn("b", "Safari", 0, 100, c.t.Add(-time.Hour), "DIRECT")))
	if d := s.Days(1)[0]; d.Total != (Bytes{35, 400}) || d.Apps["Safari"] != (Bytes{0, 100}) {
		t.Fatalf("after restart: %+v %+v", d.Total, d.Apps)
	}
	// The core restarts (Kumo started it): its counters begin at zero.
	s.NewCore(true)
	s.Record(snap(1, 2, conn("c", "git", 1, 2, c.t, "DIRECT")))
	if d := s.Days(1)[0]; d.Total != (Bytes{36, 402}) || d.Apps["git"] != (Bytes{1, 2}) {
		t.Fatalf("after core restart: %+v", d.Total)
	}
	// An unknown external core with history: only its live connections.
	s.NewCore(false)
	s.f.Totals = Bytes{}
	s.Record(snap(9e9, 9e9, conn("x", "old", 7, 7, c.t.Add(-time.Hour)), conn("y", "new", 1, 1, c.t.Add(time.Hour))))
	if d := s.Days(1)[0]; d.Total != (Bytes{37, 403}) {
		t.Fatalf("unknown core counted its history: %+v", d.Total)
	}
}

func TestRetentionClearAndCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	c := &clock{time.Date(2024, 1, 1, 12, 0, 0, 0, time.Local)}
	s := open(t, path, c)
	for i := 0; i < 1000; i++ {
		s.add(c.t, Bucket{Total: Bytes{1, 1}})
		c.t = c.t.AddDate(0, 0, 1)
	}
	c.t = c.t.AddDate(0, 0, -1)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if len(s.f.Days) != KeepDays || len(s.f.Months) != KeepMonths {
		t.Fatalf("%d days, %d months", len(s.f.Days), len(s.f.Months))
	}
	if days := s.Days(KeepDays); days[0].Total.Sum() != 2 || days[KeepDays-1].Total.Sum() != 2 {
		t.Fatal("retained days lost")
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if s2 := open(t, path, c); len(s2.f.Days) != 0 {
		t.Fatal("clear not saved")
	}
	os.WriteFile(path, []byte("{nope"), 0o600)
	if s3, err := Open(path); err == nil || s3 == nil || len(s3.f.Days) != 0 {
		t.Fatal("corrupt file not reported")
	}
}

// TestHeavyYearSize simulates a year of heavy use: 500 apps, 300 nodes and
// 40 groups seen each day, every day; the file must stay small.
func TestHeavyYearSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	c := &clock{time.Date(2025, 10, 1, 12, 0, 0, 0, time.Local)}
	s := open(t, path, c)
	for day := 0; day < 365; day++ {
		var b Bucket
		for i := range 500 {
			b.addTo(&b.Apps, fmt.Sprintf("Application Helper %03d (Renderer)", i), Bytes{int64(i) * 1e6, int64(i) * 9e6})
		}
		for i := range 300 {
			b.addTo(&b.Nodes, fmt.Sprintf("🇭🇰 Hong Kong IPLC %03d | 1.5x", i), Bytes{int64(i) * 1e6, int64(i) * 9e6})
		}
		for i := range 40 {
			b.addTo(&b.Groups, fmt.Sprintf("Streaming Group %02d", i), Bytes{1e9, 9e9})
		}
		b.Total = Bytes{5e10, 5e11}
		s.add(c.t, b)
		s.prune(c.t) // as the minute saves do
		c.t = c.t.AddDate(0, 0, 1)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	t.Logf("heavy year: %d bytes (%d days, %d months)", fi.Size(), len(s.f.Days), len(s.f.Months))
	if fi.Size() > 256<<10 {
		t.Fatalf("file %d bytes", fi.Size())
	}
	d := s.Days(1)
	_ = d
	for _, b := range s.f.Days {
		if len(b.Apps) > MaxDayKeys || len(b.Nodes) > MaxDayKeys {
			t.Fatal("day not capped")
		}
	}
}
