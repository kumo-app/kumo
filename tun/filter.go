//go:build unix

package tun

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	kcore "kumo/core"
)

// FilterEnv names the variable that turns Kumo's executable into the
// controller filter of the macOS helper; its value is the core's
// controller socket. The helper runs it as root.
const FilterEnv = "KUMO_CTL_FILTER"

// RunFilter serves the controller Kumo talks to in front of the core of
// the helper. The core listens only on a unix socket in its root-owned
// home, so nothing but the filter reaches it; the filter takes Kumo's
// secret and passes on what Kumo needs, refusing what could open the
// computer to the network or touch the system: configurations that fail
// kcore.CheckRuntime, configs loaded from a path, PATCHes beyond
// kcore.CheckPatch, core upgrades and debug endpoints.
func RunFilter(socket string) int {
	listen, secret := os.Getenv("KUMO_FILTER_LISTEN"), os.Getenv("KUMO_FILTER_SECRET")
	l, err := net.Listen("tcp", listen)
	if err != nil || secret == "" {
		fmt.Fprintln(os.Stderr, "kumo filter:", err, "(listen", listen+")")
		return 1
	}
	return serveFilter(l, socket, secret)
}

// CheckEnv names the variable that has Kumo's executable check, for the
// helper, the configuration file it names against kcore.CheckRuntime.
const CheckEnv = "KUMO_CONFIG_CHECK"

// RunCheck checks a configuration file, exiting 0 when it may be loaded.
func RunCheck(path string) int {
	data, err := os.ReadFile(path)
	if err == nil {
		err = kcore.CheckRuntime(data)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kumo:", err)
		return 3
	}
	return 0
}

func serveFilter(l net.Listener, socket, secret string) int {
	if err := http.Serve(l, filterHandler(socket, secret)); err != nil {
		return 1
	}
	return 0
}

func filterHandler(socket, secret string) http.Handler {
	target, _ := url.Parse("http://core")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, secret) {
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if err := allowed(r); err != nil {
			http.Error(w, fmt.Sprintf(`{"message":%q}`, "Kumo's Enhanced Mode helper refused this: "+err.Error()), http.StatusForbidden)
			return
		}
		r.Header.Del("Authorization")
		r.URL.RawQuery = dropToken(r.URL.Query())
		proxy.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request, secret string) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}

func dropToken(q url.Values) string {
	q.Del("token")
	return q.Encode()
}

// allowed checks a request, reading (and restoring) its body when needed.
func allowed(r *http.Request) error {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case strings.HasPrefix(path, "/upgrade"), strings.HasPrefix(path, "/debug"):
		return fmt.Errorf("%s is not available", path)
	case path == "/configs" && (r.Method == http.MethodPut || r.Method == http.MethodPost):
		var body struct {
			Path    string `json:"path"`
			Payload string `json:"payload"`
		}
		if err := readJSON(r, &body); err != nil {
			return err
		}
		if body.Payload == "" {
			return fmt.Errorf("configurations are loaded as text, not from a path")
		}
		return kcore.CheckRuntime([]byte(body.Payload))
	case path == "/configs" && r.Method == http.MethodPatch:
		body := map[string]any{}
		if err := readJSON(r, &body); err != nil {
			return err
		}
		return kcore.CheckPatch(body)
	}
	return nil
}

func readJSON(r *http.Request, v any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	return nil
}
