package monitor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecordSummaryOutages(t *testing.T) {
	m := New()
	t0 := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for i, ms := range []int{100, 120, 0, 0, 110, 300, 0, 100} {
		m.Record("HK", Sample{At: t0.Add(time.Duration(i) * time.Minute), MS: ms})
	}
	s, ok := m.Summary("HK")
	if !ok || s.Tests != 8 || s.Failures != 3 || s.Uptime != 5.0/8 || s.Down {
		t.Fatalf("%+v", s)
	}
	if s.Avg != 146 || s.P95 != 300 {
		t.Fatalf("avg %d p95 %d", s.Avg, s.P95)
	}
	// Consecutive successes: 100→120 (20), 110→300 (190).
	if s.Jitter != 105 {
		t.Fatalf("jitter %d", s.Jitter)
	}
	if !s.LastSeen.Equal(t0.Add(7 * time.Minute)) {
		t.Fatal(s.LastSeen)
	}
	o := m.Outages("HK")
	if len(o) != 2 || o[1].Failures != 2 || o[1].Duration(t0) != 2*time.Minute || o[0].Duration(t0) != time.Minute {
		t.Fatalf("%+v", o)
	}
	m.Record("HK", Sample{At: t0.Add(9 * time.Minute)})
	if s, _ := m.Summary("HK"); !s.Down || !m.Outages("HK")[0].End.IsZero() {
		t.Fatal("open outage")
	}
	if _, ok := m.Summary("nope"); ok {
		t.Fatal("unknown node")
	}
}

func TestBounded(t *testing.T) {
	m := New()
	t0 := time.Now()
	for i := range MaxSamples*3 + 7 {
		ms := 50 + i%7
		if i%2 == 0 {
			ms = 0
		}
		m.Record("n", Sample{At: t0.Add(time.Duration(i) * time.Second), MS: ms})
	}
	sm := m.Samples("n")
	if len(sm) != MaxSamples || len(m.Outages("n")) != MaxOutages {
		t.Fatalf("%d samples %d outages", len(sm), len(m.Outages("n")))
	}
	for i := 1; i < len(sm); i++ {
		if !sm[i].At.After(sm[i-1].At) {
			t.Fatal("samples out of order")
		}
	}
	if s, _ := m.Summary("n"); s.Tests != MaxSamples*3+7 {
		t.Fatal("uptime must count every test")
	}
	m.Forget(func(string) bool { return false })
	if m.Samples("n") != nil {
		t.Fatal("not forgotten")
	}
}

func TestRunStaggersAndLimits(t *testing.T) {
	m := New()
	var running, peak, calls atomic.Int32
	var mu sync.Mutex
	var urls []string
	ctx, cancel := context.WithCancel(context.Background())
	targets := []Target{{"a", "u1"}, {"b", "u2"}, {"c", "u1"}, {"d", "u1"}}
	done := make(chan struct{})
	go func() {
		m.Run(ctx, func() time.Duration { return 400 * time.Millisecond }, 2, func() []Target { return targets },
			func(ctx context.Context, tg Target) (int, error) {
				n := running.Add(1)
				for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
				}
				defer running.Add(-1)
				calls.Add(1)
				mu.Lock()
				urls = append(urls, tg.URL)
				mu.Unlock()
				time.Sleep(250 * time.Millisecond)
				if tg.Name == "b" {
					return 0, errors.New("timeout")
				}
				return 80, nil
			})
		close(done)
	}()
	time.Sleep(1100 * time.Millisecond)
	cancel()
	<-done
	if peak.Load() > 2 {
		t.Fatalf("%d tests at once", peak.Load())
	}
	if c := calls.Load(); c < 6 || c > 12 {
		t.Fatalf("%d tests in about 2.5 rounds", c)
	}
	if s, _ := m.Summary("b"); s.Failures == 0 || !s.Down {
		t.Fatalf("failure not recorded: %+v", s)
	}
	if urls[1] != "u2" {
		t.Fatal("test URL not used")
	}
}

func TestChecks(t *testing.T) {
	m := New()
	now := time.Now()
	m.SetSpeed("n", 87.5, now, nil)
	m.SetUDP("n", 1, now)
	if c := m.Checks("n"); c.Mbps != 87.5 || c.UDP != 1 {
		t.Fatalf("%+v", c)
	}
	m.SetSpeed("n", 50, now, errors.New("timeout"))
	if c := m.Checks("n"); c.Mbps != 0 || c.SpeedErr != "timeout" {
		t.Fatalf("%+v", c)
	}
	if _, ok := m.Summary("n"); ok {
		t.Fatal("checks alone are not a latency record")
	}
}
