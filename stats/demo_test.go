package stats

import (
	"math/rand"
	"os"
	"testing"
	"time"
)

// TestWriteDemo writes 90 days of made-up traffic to $KUMO_STATS_DEMO, for
// screenshots of the Statistics page.
func TestWriteDemo(t *testing.T) {
	path := os.Getenv("KUMO_STATS_DEMO")
	if path == "" {
		t.Skip("KUMO_STATS_DEMO not set")
	}
	s, _ := Open(path)
	r := rand.New(rand.NewSource(7))
	apps := []string{"Safari", "Telegram", "Spotify", "Slack", "git", "Steam", "Zoom", "curl", ""}
	nodes := []string{"Tokyo 01", "Tokyo 02", "HK 04", "Singapore 03", "DIRECT"}
	groups := map[string][]string{"Tokyo 01": {"Auto", "Proxy"}, "Tokyo 02": {"Auto", "Proxy"}, "HK 04": {"Proxy"}, "Singapore 03": {"Proxy"}}
	now := time.Now()
	for d := 400; d >= 1; d-- {
		day := now.AddDate(0, 0, -d)
		var b Bucket
		scale := 0.4 + r.Float64()
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			scale *= 1.8
		}
		for i, a := range apps {
			v := Bytes{int64(scale * float64(8-i) * 2e7 * r.Float64()), int64(scale * float64(9-i) * 2.4e8 * r.Float64())}
			n := nodes[r.Intn(len(nodes))]
			b.addTo(&b.Apps, a, v)
			b.addTo(&b.Nodes, n, v)
			for _, g := range groups[n] {
				b.addTo(&b.Groups, g, v)
			}
			b.Total.add(v)
		}
		s.add(day, b)
	}
	s.now = func() time.Time { return now }
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
}
