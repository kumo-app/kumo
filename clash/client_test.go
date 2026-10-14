package clash

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// live returns a client for CLASH_TEST_CONTROLLER (host:port), with the
// secret in CLASH_TEST_SECRET, or skips the test.
func live(t *testing.T) *Client {
	addr := os.Getenv("CLASH_TEST_CONTROLLER")
	if addr == "" {
		t.Skip("set CLASH_TEST_CONTROLLER=host:port to test against a running core")
	}
	host, port, _ := strings.Cut(addr, ":")
	n, _ := strconv.Atoi(port)
	return &Client{Host: host, Port: n, Secret: os.Getenv("CLASH_TEST_SECRET")}
}

func TestLive(t *testing.T) {
	c := live(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := c.Version(ctx); err != nil {
		t.Fatal(err)
	}
	cfg, err := c.Configs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"global", cfg.Mode} {
		if err := c.PatchConfigs(ctx, map[string]any{"mode": m}); err != nil {
			t.Fatal(err)
		}
		if got, _ := c.Configs(ctx); got.Mode != m {
			t.Fatalf("mode %q, want %q", got.Mode, m)
		}
	}
	px, err := c.Proxies(ctx)
	if err != nil || len(px) == 0 {
		t.Fatal(px, err)
	}
	for name, g := range px {
		if g.Type == "Selector" && len(g.All) > 1 {
			if err := c.Select(ctx, name, g.All[1]); err != nil {
				t.Fatal(err)
			}
			if got, _ := c.Proxies(ctx); got[name].Now != g.All[1] {
				t.Fatalf("%s: now %q", name, got[name].Now)
			}
			c.Select(ctx, name, g.Now)
			break
		}
	}
	if _, err := c.Rules(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connections(ctx); err != nil {
		t.Fatal(err)
	}
	sctx, scancel := context.WithTimeout(ctx, 3*time.Second)
	defer scancel()
	got := 0
	Stream(sctx, c, "/traffic", nil, func(Traffic) { got++ })
	if got == 0 {
		t.Fatal("no traffic over the WebSocket")
	}
}
