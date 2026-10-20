package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"kumo/clash"
)

// "Why this route": the rule a connection matched and the chain it took,
// in plain words, and a test of where a host would go.

// route is a connection's way out: the rule it matched and the chain,
// from the rule's target to the node.
type route struct {
	rule, payload string
	chain         []string // outermost group first, the node last
}

func routeOf(cn clash.Connection) route {
	chain := slices.Clone(cn.Chains)
	slices.Reverse(chain)
	return route{rule: cn.Rule, payload: cn.Payload, chain: chain}
}

// ruleNames are mihomo's rule types, as its connections and logs name
// them, in the configuration's terms.
var ruleNames = map[string]string{
	"domain": "DOMAIN", "domainsuffix": "DOMAIN-SUFFIX", "domainkeyword": "DOMAIN-KEYWORD", "domainregex": "DOMAIN-REGEX",
	"domainwildcard": "DOMAIN-WILDCARD", "geosite": "GEOSITE", "geoip": "GEOIP", "srcgeoip": "SRC-GEOIP",
	"ipcidr": "IP-CIDR", "ipcidr6": "IP-CIDR6", "srcipcidr": "SRC-IP-CIDR", "ipsuffix": "IP-SUFFIX", "ipasn": "IP-ASN",
	"srcport": "SRC-PORT", "dstport": "DST-PORT", "inport": "IN-PORT", "intype": "IN-TYPE", "processname": "PROCESS-NAME",
	"processpath": "PROCESS-PATH", "processnameregex": "PROCESS-NAME-REGEX", "network": "NETWORK", "ruleset": "RULE-SET",
	"match": "MATCH", "and": "AND", "or": "OR", "not": "NOT", "subrules": "SUB-RULE",
}

// ruleName is a rule type the way a configuration writes it.
func ruleName(t string) string {
	if n, ok := ruleNames[strings.ToLower(strings.ReplaceAll(t, "-", ""))]; ok {
		return n
	}
	return strings.ToUpper(t)
}

// matched is the rule as a configuration line: "DOMAIN-SUFFIX,google.com".
func (r route) matched() string {
	if r.rule == "" {
		return ""
	}
	if r.payload == "" {
		return ruleName(r.rule)
	}
	return ruleName(r.rule) + "," + r.payload
}

// node is where the connection leaves: the chain's last.
func (r route) node() string {
	if len(r.chain) == 0 {
		return ""
	}
	return r.chain[len(r.chain)-1]
}

// explain says the route in plain words.
func (r route) explain(mode string) string {
	node := r.node()
	var how string
	switch {
	case mode == "global":
		how = tr("Global mode sends everything to GLOBAL")
	case mode == "direct":
		how = tr("Direct mode sends everything out directly")
	case r.rule == "" || strings.EqualFold(r.rule, "match"):
		how = tr("No other rule matched, so the final rule (MATCH) decided")
	default:
		how = fmt.Sprintf(tr("Matched %s"), r.matched())
	}
	var path []string
	for i, n := range r.chain {
		switch {
		case n == "DIRECT":
			path = append(path, tr("out directly, without a node"))
		case n == "REJECT" || n == "REJECT-DROP":
			path = append(path, tr("blocked"))
		case i < len(r.chain)-1:
			path = append(path, tr("group")+" "+n)
		default:
			path = append(path, tr("node")+" "+n)
		}
	}
	if node == "" {
		return how
	}
	return how + " → " + strings.Join(path, " → ")
}

// routeTest is a test of where a host goes.
type routeTest struct {
	input   string
	running bool
	done    bool
	target  string
	found   bool
	route   route
	status  int
	err     string
	elapsed time.Duration
	from    string // "connection" or "log"
}

// testURL makes a URL of what the user typed: a host, host:port or URL.
func testURL(in string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", fmt.Errorf("%s", tr("Enter a domain or a URL"))
	}
	if !strings.Contains(in, "://") {
		in = "https://" + in
	}
	u, err := url.Parse(in)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%s", tr("Enter a domain or a URL"))
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// logRoute reads mihomo's info line of a connection from the source port:
// "[TCP] 127.0.0.1:54321(curl, uid=501) --> example.com:443 match DomainSuffix(example.com) using Proxy[HK-01]",
// "... match Match using PROXY[N1]", "... doesn't match any rule using DIRECT";
// and the line of one that failed to dial:
// "[TCP] dial Proxy (match DomainSuffix/example.com) 127.0.0.1:54321(curl) --> example.com:443 error: ...".
var (
	logRoute = regexp.MustCompile(`(?:127\.0\.0\.1|\[::1\]|localhost):(\d+)(?:\([^)]*\))? --> \S+ (?:match (\S+?)(?:\((.*?)\))? using|doesn't match any rule using) (.+)$`)
	logDial  = regexp.MustCompile(`dial (.+?) \(match (\w+)/(.*?)\) (?:127\.0\.0\.1|\[::1\]|localhost):(\d+)(?:\([^)]*\))? --> \S+ error: (.*)$`)
)

func routeFromLog(line string, port int) (route, bool) {
	var r route
	var using string
	if m := logRoute.FindStringSubmatch(line); m != nil && m[1] == strconv.Itoa(port) {
		r.rule, r.payload, using = m[2], m[3], m[4]
	} else if m := logDial.FindStringSubmatch(line); m != nil && m[4] == strconv.Itoa(port) {
		r.rule, r.payload, using = m[2], m[3], m[1]
	} else {
		return route{}, false
	}
	if r.rule == "" {
		r.rule = "Match"
	}
	// "Proxy[HK-01]", "Proxy[Auto[HK-01]]", or "DIRECT".
	using = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(using), `"`))
	for using != "" {
		name, rest, ok := strings.Cut(using, "[")
		r.chain = append(r.chain, strings.TrimSpace(name))
		if !ok {
			break
		}
		using = strings.TrimSuffix(rest, "]")
	}
	return r, true
}

// runRouteTest requests the URL through Kumo, finds the core's
// connection for it by the source port, then closes it.
func (k *Kumo) runRouteTest() {
	t := &k.routeTest
	target, err := testURL(t.input)
	if err != nil {
		*t = routeTest{input: t.input, done: true, err: err.Error()}
		return
	}
	*t = routeTest{input: t.input, running: true, target: target}
	cli, port := k.cli, k.listenPort()
	go func() {
		res := probeRoute(cli, port, target)
		if !res.found {
			// Its log line comes over the log feed, in batches.
			time.Sleep(2 * logEvery)
		}
		k.post(func() {
			if res.target != k.routeTest.target {
				return
			}
			if !res.found {
				// The core may have logged the route of a request that
				// failed before it was listed.
				for _, l := range slices.Backward(k.logs) {
					for _, p := range res.ports {
						if r, ok := routeFromLog(l.Payload, p); ok {
							res.found, res.route, res.from = true, r, "log"
						}
					}
					if res.found {
						break
					}
				}
			}
			res.input = k.routeTest.input
			res.running, res.done = false, true
			k.routeTest = res.routeTest
		})
	}()
}

type probe struct {
	routeTest
	ports []int
}

// probeRoute makes the request and watches the core's connections for it.
func probeRoute(cli *clash.Client, port int, target string) probe {
	res := probe{routeTest: routeTest{target: target, from: "connection"}}
	var mu sync.Mutex
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tp := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := dialer.DialContext(ctx, network, addr)
			if err == nil {
				if a, ok := c.LocalAddr().(*net.TCPAddr); ok {
					mu.Lock()
					res.ports = append(res.ports, a.Port)
					mu.Unlock()
				}
			}
			return c, err
		},
		ResponseHeaderTimeout: 10 * time.Second,
	}
	defer tp.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		req, err := http.NewRequestWithContext(ctx, "HEAD", target, nil)
		if err != nil {
			res.err = err.Error()
			return
		}
		req.Header.Set("User-Agent", "Kumo/"+version+" route test")
		r, err := (&http.Client{Transport: tp, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			res.err = err.Error()
			return
		}
		io.Copy(io.Discard, io.LimitReader(r.Body, 4<<10))
		r.Body.Close()
		res.status = r.StatusCode
	}()
	// Look for the connection while the request runs, and once more after
	// it answered: the tunnel stays open, idle, until closed below.
	var found clash.Connection
	look := func() bool {
		if cli == nil {
			return false
		}
		lctx, lcancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer lcancel()
		conns, err := cli.Connections(lctx)
		if err != nil {
			return false
		}
		mu.Lock()
		ports := slices.Clone(res.ports)
		mu.Unlock()
		for _, cn := range conns.Connections {
			p, _ := strconv.Atoi(cn.Metadata.SourcePort)
			if slices.Contains(ports, p) && cn.Metadata.Type != "Inner" {
				found = cn
				return true
			}
		}
		return false
	}
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	finished := false
	for !finished && !res.found {
		select {
		case <-done:
			finished = true
		case <-tick.C:
		}
		res.found = look()
	}
	<-done
	res.elapsed = time.Since(start)
	if res.found {
		res.route = routeOf(found)
		// Close it cleanly: the transport's idle tunnel, and the core's side.
		tp.CloseIdleConnections()
		cctx, ccancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = cli.CloseConnection(cctx, found.ID)
		ccancel()
	}
	return res
}
