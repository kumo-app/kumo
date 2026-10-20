// Package monitor watches the nodes' latency for as long as Kumo runs, the
// way uptime probes do: periodic tests, a bounded record of samples and
// outages per node in memory, and their summary. Nothing is written to
// disk.
package monitor

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// Bounds of what is kept per node.
const (
	MaxSamples = 720 // 12 hours at the default interval
	MaxOutages = 100
)

// Sample is one test: its latency in milliseconds, 0 when it failed.
type Sample struct {
	At time.Time
	MS int
}

// OK says the test succeeded.
func (s Sample) OK() bool { return s.MS > 0 }

// Outage is a run of failed tests; End is zero while it lasts.
type Outage struct {
	Start, End time.Time
	Failures   int
}

// Duration is how long the outage lasted, or has lasted until now.
func (o Outage) Duration(now time.Time) time.Duration {
	if o.End.IsZero() {
		return now.Sub(o.Start)
	}
	return o.End.Sub(o.Start)
}

// Summary sums up a node's record.
type Summary struct {
	Tests, Failures int
	// Uptime is the share of successful tests, 0 to 1.
	Uptime float64
	// Avg, P95 and Jitter (mean change between consecutive successes) in
	// milliseconds.
	Avg, P95, Jitter int
	LastSeen         time.Time // the last success
	Down             bool      // the last test failed
	Checks
}

// Checks are the node's manual tests: download speed and UDP.
type Checks struct {
	Mbps     float64 // 0: not measured, or failed (SpeedErr)
	SpeedAt  time.Time
	SpeedErr string
	// UDP is 0 unknown/untested, 1 supported, 2 unsupported
	// (nodetest.UDP).
	UDP   int
	UDPAt time.Time
}

type node struct {
	samples []Sample // ring
	head    int      // next write position once full
	outages []Outage
	tests   int
	fails   int
	last    time.Time
	checks  Checks
}

// Monitor keeps the record of every node. It is safe for concurrent use.
type Monitor struct {
	mu    sync.Mutex
	nodes map[string]*node
}

// New returns an empty Monitor.
func New() *Monitor { return &Monitor{nodes: map[string]*node{}} }

// Record adds a test of name.
func (m *Monitor) Record(name string, s Sample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.node(name)
	if len(n.samples) < MaxSamples {
		n.samples = append(n.samples, s)
	} else {
		n.samples[n.head] = s
		n.head = (n.head + 1) % MaxSamples
	}
	n.tests++
	open := len(n.outages) > 0 && n.outages[len(n.outages)-1].End.IsZero()
	if s.OK() {
		n.last = s.At
		if open {
			n.outages[len(n.outages)-1].End = s.At
		}
		return
	}
	n.fails++
	if open {
		n.outages[len(n.outages)-1].Failures++
		return
	}
	n.outages = append(n.outages, Outage{Start: s.At, Failures: 1})
	if len(n.outages) > MaxOutages {
		n.outages = slices.Delete(n.outages, 0, len(n.outages)-MaxOutages)
	}
}

func (m *Monitor) node(name string) *node {
	n := m.nodes[name]
	if n == nil {
		n = &node{}
		m.nodes[name] = n
	}
	return n
}

// SetSpeed records a download test of name; err is its failure.
func (m *Monitor) SetSpeed(name string, mbps float64, at time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := &m.node(name).checks
	c.Mbps, c.SpeedAt, c.SpeedErr = mbps, at, ""
	if err != nil {
		c.Mbps, c.SpeedErr = 0, err.Error()
	}
}

// SetUDP records a UDP check of name.
func (m *Monitor) SetUDP(name string, udp int, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := &m.node(name).checks
	c.UDP, c.UDPAt = udp, at
}

// Checks returns name's manual tests.
func (m *Monitor) Checks(name string) Checks {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := m.nodes[name]; n != nil {
		return n.checks
	}
	return Checks{}
}

// Samples returns name's kept samples, oldest first.
func (m *Monitor) Samples(name string) []Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[name]
	if n == nil {
		return nil
	}
	return append(slices.Clone(n.samples[n.head:]), n.samples[:n.head]...)
}

// Outages returns name's outages, newest first.
func (m *Monitor) Outages(name string) []Outage {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[name]
	if n == nil {
		return nil
	}
	out := slices.Clone(n.outages)
	slices.Reverse(out)
	return out
}

// Uptime is the share of name's successful tests and their count, false
// before its first test: what a node's card shows, without the copy and
// sort of the samples that Summary does.
func (m *Monitor) Uptime(name string) (uptime float64, tests int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[name]
	if n == nil || n.tests == 0 {
		return 0, 0, false
	}
	return float64(n.tests-n.fails) / float64(n.tests), n.tests, true
}

// Summary sums up name's record: uptime over every test since launch,
// latency over the samples kept.
func (m *Monitor) Summary(name string) (Summary, bool) {
	samples := m.Samples(name)
	m.mu.Lock()
	n := m.nodes[name]
	var sum Summary
	if n != nil {
		sum.Tests, sum.Failures, sum.LastSeen, sum.Checks = n.tests, n.fails, n.last, n.checks
	}
	m.mu.Unlock()
	if n == nil || sum.Tests == 0 {
		return sum, false
	}
	sum.Uptime = float64(sum.Tests-sum.Failures) / float64(sum.Tests)
	if len(samples) > 0 {
		sum.Down = !samples[len(samples)-1].OK()
	}
	var ok []int
	var jit, pairs, prev int
	for _, s := range samples {
		if !s.OK() {
			prev = 0
			continue
		}
		ok = append(ok, s.MS)
		if prev > 0 {
			jit += abs(s.MS - prev)
			pairs++
		}
		prev = s.MS
	}
	if len(ok) > 0 {
		total := 0
		for _, v := range ok {
			total += v
		}
		sum.Avg = total / len(ok)
		sorted := slices.Clone(ok)
		slices.Sort(sorted)
		sum.P95 = sorted[(len(sorted)*95+99)/100-1]
	}
	if pairs > 0 {
		sum.Jitter = jit / pairs
	}
	return sum, true
}

// Forget drops nodes not in keep, as when a profile changes.
func (m *Monitor) Forget(keep func(string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range m.nodes {
		if !keep(name) {
			delete(m.nodes, name)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Target is a node to test and the URL to test it with.
type Target struct {
	Name, URL string
}

// ErrSkip from a Probe means the test could not be made (the core did not
// answer); it is not recorded against the node.
var ErrSkip = errors.New("monitor: test skipped")

// Probe tests one node, returning its latency in milliseconds.
type Probe func(ctx context.Context, t Target) (int, error)

// Run tests the targets every interval until ctx ends. Each round spreads
// its tests over the interval, at most parallel at once, so a long list
// never arrives at the core, or the nodes, in one burst. targets is asked
// anew each round.
func (m *Monitor) Run(ctx context.Context, interval func() time.Duration, parallel int, targets func() []Target, probe Probe) {
	sem := make(chan struct{}, max(parallel, 1))
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		every := interval()
		if every <= 0 {
			if !sleep(ctx, 5*time.Second) {
				return
			}
			continue
		}
		list := targets()
		gap := every / time.Duration(max(len(list), 1))
		gap = min(max(gap, 100*time.Millisecond), 5*time.Second)
		start := time.Now()
		for _, t := range list {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Add(1)
			go func(t Target) {
				defer func() { <-sem; wg.Done() }()
				ms, err := probe(ctx, t)
				if ctx.Err() != nil {
					return
				}
				if errors.Is(err, ErrSkip) {
					return
				}
				if err != nil {
					ms = 0
				}
				m.Record(t.Name, Sample{At: time.Now(), MS: ms})
			}(t)
			if !sleep(ctx, gap) {
				return
			}
		}
		if !sleep(ctx, every-time.Since(start)) {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
