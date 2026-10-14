//go:build unix

package tun

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilter(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctl.sock")
	ul, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	go http.Serve(ul, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path+" auth="+r.Header.Get("Authorization"))
		io.WriteString(w, `{"ok":true}`)
	}))
	tl, _ := net.Listen("tcp", "127.0.0.1:0")
	go serveFilter(tl, sock, "kumo")
	base := "http://" + tl.Addr().String()
	do := func(method, path, auth string, body any) int {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req, _ := http.NewRequest(method, base+path, rd)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	good := "mixed-port: 7890\nproxies: []\nrules:\n  - MATCH,DIRECT\ntun:\n  enable: true\n  stack: mixed\n"
	for _, c := range []struct {
		method, path, auth string
		body               any
		want               int
	}{
		{"GET", "/version", "", nil, 401},
		{"GET", "/version", "wrong", nil, 401},
		{"GET", "/version", "kumo", nil, 200},
		{"PUT", "/proxies/G", "kumo", map[string]string{"name": "a"}, 200},
		{"PUT", "/configs", "kumo", map[string]string{"payload": good}, 200},
		{"PUT", "/configs", "kumo", map[string]string{"payload": good + "listeners: []\n"}, 403},
		{"PUT", "/configs", "kumo", map[string]string{"path": "/etc/x.yaml"}, 403},
		{"PATCH", "/configs", "kumo", map[string]any{"mode": "global"}, 200},
		{"PATCH", "/configs", "kumo", map[string]any{"redir-port": 7892}, 403},
		{"PATCH", "/configs", "kumo", map[string]any{"tun": map[string]any{"enable": true, "device": "x"}}, 403},
		{"POST", "/upgrade", "kumo", map[string]any{}, 403},
		{"GET", "/debug/pprof", "kumo", nil, 403},
	} {
		if code := do(c.method, c.path, c.auth, c.body); code != c.want {
			t.Errorf("%s %s (auth %q): %d, want %d", c.method, c.path, c.auth, code, c.want)
		}
	}
	for _, g := range got {
		if strings.Contains(g, "auth=Bearer") {
			t.Errorf("Kumo's secret passed to the core: %s", g)
		}
	}
}
