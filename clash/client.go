// Package clash talks to the external controller of a Clash or mihomo core:
// its REST API and its WebSocket streams. It imports nothing of the UI.
package clash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Client is a controller at Host:Port, authorized by Secret. With TLS it
// speaks https and wss, which a controller on another machine should use:
// the secret travels with every request.
type Client struct {
	Host   string
	Port   int
	Secret string
	TLS    bool
	HTTP   *http.Client
}

func (c *Client) base(scheme string) string {
	if c.TLS {
		scheme += "s"
	}
	return fmt.Sprintf("%s://%s", scheme, netJoin(c.Host, c.Port))
}

// Loopback says whether host is this machine.
func Loopback(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func netJoin(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(port)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base("http")+path, rd)
	if err != nil {
		return err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		var e struct{ Message string }
		if json.Unmarshal(msg, &e) == nil && e.Message != "" {
			return &StatusError{res.StatusCode, fmt.Sprintf("%s: %s", res.Status, e.Message)}
		}
		return &StatusError{res.StatusCode, fmt.Sprintf("%s %s: %s", method, path, res.Status)}
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

// StatusError is an answer of the controller other than success, as
// opposed to no answer at all.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return e.Msg }

// Version is the core's version.
type Version struct {
	Meta    bool   `json:"meta"`
	Version string `json:"version"`
}

func (c *Client) Version(ctx context.Context) (v Version, err error) {
	err = c.do(ctx, "GET", "/version", nil, &v)
	return
}

// Config is the part of /configs the app shows and edits.
type Config struct {
	Port        int    `json:"port"`
	SocksPort   int    `json:"socks-port"`
	MixedPort   int    `json:"mixed-port"`
	RedirPort   int    `json:"redir-port"`
	TProxyPort  int    `json:"tproxy-port"`
	AllowLan    bool   `json:"allow-lan"`
	Mode        string `json:"mode"`
	LogLevel    string `json:"log-level"`
	IPv6        bool   `json:"ipv6"`
	BindAddress string `json:"bind-address"`
	Tun         struct {
		Enable bool `json:"enable"`
	} `json:"tun"`
}

func (c *Client) Configs(ctx context.Context) (cfg Config, err error) {
	err = c.do(ctx, "GET", "/configs", nil, &cfg)
	return
}

// PatchConfigs changes the given keys of the running configuration.
func (c *Client) PatchConfigs(ctx context.Context, patch map[string]any) error {
	return c.do(ctx, "PATCH", "/configs", patch, nil)
}

// ReloadConfigs has the core load the configuration in data. It is sent
// as text, so the core reads no file it is pointed at: the Enhanced Mode
// core on macOS runs as root, in a home of its own.
func (c *Client) ReloadConfigs(ctx context.Context, data []byte) error {
	return c.do(ctx, "PUT", "/configs?force=true", map[string]string{"payload": string(data)}, nil)
}

// Delay is one latency test.
type Delay struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"`
}

// Proxy is a proxy or a group of them.
type Proxy struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	All     []string `json:"all"`
	UDP     bool     `json:"udp"`
	Hidden  bool     `json:"hidden"`
	TestURL string   `json:"testUrl"`
	History []Delay  `json:"history"`
}

// LastDelay is the latest latency, 0 for none or a failure.
func (p Proxy) LastDelay() int {
	if n := len(p.History); n > 0 {
		return p.History[n-1].Delay
	}
	return 0
}

// IsGroup says that the proxy chooses among others.
func (p Proxy) IsGroup() bool { return len(p.All) > 0 }

func (c *Client) Proxies(ctx context.Context) (map[string]Proxy, error) {
	var out struct{ Proxies map[string]Proxy }
	err := c.do(ctx, "GET", "/proxies", nil, &out)
	return out.Proxies, err
}

// Select has a selector group use the proxy name.
func (c *Client) Select(ctx context.Context, group, name string) error {
	return c.do(ctx, "PUT", "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
}

// TestDelay measures a proxy against testURL, in milliseconds.
func (c *Client) TestDelay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error) {
	q := url.Values{"url": {testURL}, "timeout": {strconv.Itoa(int(timeout.Milliseconds()))}}
	var out struct{ Delay int }
	err := c.do(ctx, "GET", "/proxies/"+url.PathEscape(name)+"/delay?"+q.Encode(), nil, &out)
	return out.Delay, err
}

// Rule is a rule of the running configuration.
type Rule struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
}

func (c *Client) Rules(ctx context.Context) ([]Rule, error) {
	var out struct{ Rules []Rule }
	err := c.do(ctx, "GET", "/rules", nil, &out)
	return out.Rules, err
}

// Connection is a connection the core carries.
type Connection struct {
	ID       string    `json:"id"`
	Upload   int64     `json:"upload"`
	Download int64     `json:"download"`
	Start    time.Time `json:"start"`
	Chains   []string  `json:"chains"`
	Rule     string    `json:"rule"`
	Payload  string    `json:"rulePayload"`
	Metadata struct {
		Network  string `json:"network"`
		Type     string `json:"type"`
		SourceIP string `json:"sourceIP"`
		DestIP   string `json:"destinationIP"`
		DestPort string `json:"destinationPort"`
		Host     string `json:"host"`
		Process  string `json:"process"`
	} `json:"metadata"`
}

// Target is where the connection goes, host or address, with the port.
func (c Connection) Target() string {
	h := c.Metadata.Host
	if h == "" {
		h = c.Metadata.DestIP
	}
	return h + ":" + c.Metadata.DestPort
}

// Connections is a snapshot of /connections.
type Connections struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
	Memory        int64        `json:"memory"`
}

func (c *Client) Connections(ctx context.Context) (out Connections, err error) {
	err = c.do(ctx, "GET", "/connections", nil, &out)
	return
}

func (c *Client) CloseConnection(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/connections/"+url.PathEscape(id), nil, nil)
}

func (c *Client) CloseAllConnections(ctx context.Context) error {
	return c.do(ctx, "DELETE", "/connections", nil, nil)
}

// Traffic is a second's upload and download, in bytes.
type Traffic struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// Memory is the core's memory in use, in bytes.
type Memory struct {
	InUse   int64 `json:"inuse"`
	OSLimit int64 `json:"oslimit"`
}

// Log is a line of the core's log.
type Log struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// Stream reads the WebSocket at path, decoding each message into a new T
// for fn, until ctx ends or the socket fails.
func Stream[T any](ctx context.Context, c *Client, path string, query url.Values, fn func(T)) error {
	if query == nil {
		query = url.Values{}
	}
	u := c.base("ws") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	// The secret goes in a header, never in the URL, which proxies log.
	opts := &websocket.DialOptions{HTTPHeader: http.Header{}}
	if c.Secret != "" {
		opts.HTTPHeader.Set("Authorization", "Bearer "+c.Secret)
	}
	ws, _, err := websocket.Dial(ctx, u, opts)
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	ws.SetReadLimit(32 << 20)
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		var v T
		if json.Unmarshal(data, &v) == nil {
			fn(v)
		}
	}
}
