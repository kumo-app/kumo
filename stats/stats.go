// Package stats keeps how much traffic went through the core: per day and
// per month, in total and by app, node and proxy group. It learns from the
// controller's /connections snapshots and keeps its record in one small
// gzip-compressed JSON file, written atomically: aggregates only, never
// a log of connections.
package stats

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"kumo/clash"
	"kumo/core"
)

// Retention.
const (
	KeepDays   = 90
	KeepMonths = 24
	// MaxDayKeys and MaxMonthKeys bound the apps, nodes and groups kept
	// in a day and a month; the smallest are folded into Other, so the
	// file stays small however much is used.
	MaxDayKeys   = 20
	MaxMonthKeys = 100
	// Other names what is folded away or has no name.
	Other = ""
)

// Bytes is traffic up and down.
type Bytes struct {
	Up   int64 `json:"u"`
	Down int64 `json:"d"`
}

// Sum is up plus down.
func (b Bytes) Sum() int64 { return b.Up + b.Down }

func (b *Bytes) add(o Bytes) { b.Up += o.Up; b.Down += o.Down }

// Bucket is one day's or month's traffic.
type Bucket struct {
	Total  Bytes            `json:"t"`
	Apps   map[string]Bytes `json:"a,omitempty"`
	Nodes  map[string]Bytes `json:"n,omitempty"`
	Groups map[string]Bytes `json:"g,omitempty"`
}

func (b *Bucket) addTo(m *map[string]Bytes, key string, d Bytes) {
	if *m == nil {
		*m = map[string]Bytes{}
	}
	v := (*m)[key]
	v.add(d)
	(*m)[key] = v
}

// Merge adds o into b.
func (b *Bucket) Merge(o Bucket) {
	b.Total.add(o.Total)
	for k, v := range o.Apps {
		b.addTo(&b.Apps, k, v)
	}
	for k, v := range o.Nodes {
		b.addTo(&b.Nodes, k, v)
	}
	for k, v := range o.Groups {
		b.addTo(&b.Groups, k, v)
	}
}

// file is what is saved.
type file struct {
	Version int               `json:"v"`
	Days    map[string]Bucket `json:"days"`
	Months  map[string]Bucket `json:"months"`
	// Seen is each live connection's bytes already counted, and Totals
	// the core's counters, so that a restart of Kumo does not count again
	// what a core still running already reported.
	Seen   map[string]Bytes `json:"seen,omitempty"`
	Totals Bytes            `json:"totals"`
	Saved  time.Time        `json:"saved"`
}

// Version is the format of the record this Kumo writes.
const Version = 1

// ErrNewer is Open's error for a record a newer Kumo wrote: Kumo leaves
// it as it is, and counts this session in memory only.
var ErrNewer = errors.New("the traffic statistics were saved by a newer version of Kumo")

// Store records traffic. It is safe for use by several goroutines.
type Store struct {
	mu    sync.Mutex
	path  string
	f     file
	dirty bool
	fresh bool // the next snapshot is the first from a core
	// started says Kumo started that core, so all its traffic is new.
	started bool
	// readOnly keeps a newer Kumo's record from being overwritten.
	readOnly bool
	now      func() time.Time
}

// Open reads the record at path, starting an empty one when there is none.
// A record that cannot be read is kept aside (core.KeepCorrupt) and the
// error returned with a usable empty Store.
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, fresh: true}
	s.reset()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	data, err := gunzip(raw)
	if err == nil {
		err = json.Unmarshal(data, &s.f)
	}
	if err != nil {
		s.reset()
		core.KeepCorrupt(path)
		return s, err
	}
	if s.f.Version > Version {
		s.reset()
		s.readOnly = true
		return s, ErrNewer
	}
	// Version 0 had no field, and the same layout: nothing to migrate.
	s.f.Version = Version
	if s.f.Days == nil {
		s.f.Days = map[string]Bucket{}
	}
	if s.f.Months == nil {
		s.f.Months = map[string]Bucket{}
	}
	return s, nil
}

func (s *Store) reset() {
	s.f = file{Version: Version, Days: map[string]Bucket{}, Months: map[string]Bucket{}}
}

// NewCore tells the store that the next snapshot comes from a core that
// may not be the one before (Kumo started, reconnected, or restarted the
// core). started says Kumo has just started that core itself, so all its
// traffic is new.
func (s *Store) NewCore(started bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fresh, s.started = true, started
	if started {
		s.f.Seen, s.f.Totals = nil, Bytes{}
	}
}

// Record takes a /connections snapshot and counts what is new in it.
//
// The core's own counters give the total exactly, closed connections
// included. Live connections give the split by app, node and group: each
// one's growth since the last snapshot. A connection that closes between
// two snapshots takes its last moments' bytes with it; they count in the
// total and under Other.
func (s *Store) Record(c clash.Connections) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var split Bucket
	seen := make(map[string]Bytes, len(c.Connections))
	for _, cn := range c.Connections {
		cur := Bytes{cn.Upload, cn.Download}
		seen[cn.ID] = cur
		last, known := s.f.Seen[cn.ID]
		switch {
		case known && cur.Up >= last.Up && cur.Down >= last.Down:
			cur = Bytes{cur.Up - last.Up, cur.Down - last.Down}
		case known:
			// The counters went back: a new connection with an old ID.
		case s.fresh && cn.Start.Before(s.f.Saved):
			// Already running when last saved, and not known: its bytes
			// may already be counted, so start from here.
			continue
		}
		if cur.Sum() == 0 {
			continue
		}
		app := cn.Metadata.Process
		node, groups := Other, []string(nil)
		if len(cn.Chains) > 0 {
			node, groups = cn.Chains[0], cn.Chains[1:]
		}
		split.Total.add(cur)
		split.addTo(&split.Apps, app, cur)
		split.addTo(&split.Nodes, node, cur)
		for _, g := range groups {
			split.addTo(&split.Groups, g, cur)
		}
	}
	tot := Bytes{c.UploadTotal, c.DownloadTotal}
	var delta Bytes
	switch {
	case s.fresh && s.started:
		delta = tot // a core Kumo started: everything is new
	case s.fresh && (s.f.Totals == (Bytes{}) || tot.Up < s.f.Totals.Up || tot.Down < s.f.Totals.Down):
		// A core of unknown history, or another than the one last seen:
		// count only what its live connections show.
		delta = split.Total
	case tot.Up < s.f.Totals.Up || tot.Down < s.f.Totals.Down:
		delta = tot // the counters were reset
	default:
		delta = Bytes{tot.Up - s.f.Totals.Up, tot.Down - s.f.Totals.Down}
	}
	// The connections cannot have carried more than the core counted.
	delta.Up, delta.Down = max(delta.Up, split.Total.Up), max(delta.Down, split.Total.Down)
	if rest := (Bytes{delta.Up - split.Total.Up, delta.Down - split.Total.Down}); rest.Sum() > 0 {
		split.addTo(&split.Apps, Other, rest)
		split.addTo(&split.Nodes, Other, rest)
	}
	split.Total = delta
	s.f.Seen, s.f.Totals, s.fresh, s.started = seen, tot, false, false
	if delta.Sum() == 0 {
		return
	}

	s.add(now, split)
}

func (s *Store) add(now time.Time, b Bucket) {
	day, month := now.Format("2006-01-02"), now.Format("2006-01")
	d := s.f.Days[day]
	d.Merge(b)
	s.f.Days[day] = d
	m := s.f.Months[month]
	m.Merge(b)
	s.f.Months[month] = m
	s.dirty = true
}

// prune drops days and months past retention and folds the smallest keys.
func (s *Store) prune(now time.Time) {
	firstDay := now.AddDate(0, 0, -(KeepDays - 1)).Format("2006-01-02")
	for k := range s.f.Days {
		if k < firstDay {
			delete(s.f.Days, k)
		}
	}
	y, m, _ := now.Date()
	firstMonth := time.Date(y, m-(KeepMonths-1), 1, 0, 0, 0, 0, now.Location()).Format("2006-01")
	for k := range s.f.Months {
		if k < firstMonth {
			delete(s.f.Months, k)
		}
	}
	for all, n := range map[*map[string]Bucket]int{&s.f.Days: MaxDayKeys, &s.f.Months: MaxMonthKeys} {
		for k, b := range *all {
			b.Apps, b.Nodes, b.Groups = fold(b.Apps, n), fold(b.Nodes, n), fold(b.Groups, n)
			(*all)[k] = b
		}
	}
}

func fold(m map[string]Bytes, max int) map[string]Bytes {
	if len(m) <= max {
		return m
	}
	top := Top(m, 0)
	out := make(map[string]Bytes, max)
	var rest Bytes
	for _, e := range top {
		if len(out) < max-1 && e.Name != Other {
			out[e.Name] = e.Bytes
		} else {
			rest.add(e.Bytes)
		}
	}
	out[Other] = rest
	return out
}

// Save writes the record if it changed.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || s.readOnly {
		return nil
	}
	now := s.now()
	s.prune(now)
	s.f.Saved = now
	js, err := json.Marshal(s.f)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(js)
	if err := zw.Close(); err != nil {
		return err
	}
	data := buf.Bytes()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := core.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// Clear forgets all traffic and saves at once.
func (s *Store) Clear() error {
	s.mu.Lock()
	seen, tot := s.f.Seen, s.f.Totals
	s.reset()
	s.f.Seen, s.f.Totals, s.dirty = seen, tot, true
	s.mu.Unlock()
	return s.Save()
}

// Day is one day's traffic.
type Day struct {
	Date time.Time
	Bucket
}

// Days returns the last n days, oldest first, today last, empty days
// included.
func (s *Store) Days(n int) []Day {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	y, m, d := now.Date()
	out := make([]Day, n)
	for i := range n {
		t := time.Date(y, m, d-(n-1-i), 0, 0, 0, 0, now.Location())
		out[i] = Day{t, s.f.Days[t.Format("2006-01-02")]}
	}
	return out
}

// Month is one month's traffic.
type Month struct {
	Date time.Time
	Bucket
}

// Months returns the last n months, oldest first, this month last.
func (s *Store) Months(n int) []Month {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	y, m, _ := now.Date()
	out := make([]Month, n)
	for i := range n {
		t := time.Date(y, m-time.Month(n-1-i), 1, 0, 0, 0, 0, now.Location())
		out[i] = Month{t, s.f.Months[t.Format("2006-01")]}
	}
	return out
}

// Entry is a named amount of traffic.
type Entry struct {
	Name string
	Bytes
}

// Top lists m's entries by traffic, largest first, at most n (0: all).
func Top(m map[string]Bytes, n int) []Entry {
	out := make([]Entry, 0, len(m))
	for k, v := range m {
		out = append(out, Entry{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].Sum(), out[j].Sum(); a != b {
			return a > b
		}
		return out[i].Name < out[j].Name
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// Total merges the buckets of days.
func Total(days []Day) Bucket {
	var b Bucket
	for _, d := range days {
		b.Merge(d.Bucket)
	}
	return b
}

// gunzip reads a gzip-compressed record, or a plain JSON one.
func gunzip(raw []byte) ([]byte, error) {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return raw, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(zr, 64<<20))
}
