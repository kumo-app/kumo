package sysproxy

import (
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"strings"
)

// Effective is the proxy the system applies now, as apps that follow the
// system settings see it: the manual proxies in force ("host:port", empty
// when off), and the automatic configuration that decides before them.
type Effective struct {
	HTTP, HTTPS, SOCKS string
	PAC                string // the URL of a proxy auto-config script in use
	AutoDetect         bool   // proxy auto-discovery (WPAD) is on
	Source             string // what was read: "scutil --proxy", "WinINet", "gsettings", "environment"
}

// ErrUnknown is Read's error where the system's proxy cannot be read.
var ErrUnknown = errors.New("the system proxy settings cannot be read here")

// Read reads the effective proxy of the system: on macOS the primary
// network service's, which `scutil --proxy` reports, PAC and discovery
// included; on Windows WinINet's, AutoConfigURL and AutoDetect included;
// on Linux GNOME's settings, else the proxy variables of Kumo's
// environment (best effort).
func Read() (Effective, error) { return readEffective() }

// Cause is why the system's proxy does not send apps to Kumo.
type Cause int

const (
	Applied    Cause = iota // HTTP and HTTPS point at Kumo
	Off                     // no proxy is on
	Elsewhere               // a proxy is on, at another address
	PAC                     // an auto-config script decides first
	AutoDetect              // auto-discovery may pick another proxy first
	Partial                 // only some protocols point at Kumo
)

func (c Cause) String() string {
	return [...]string{"applied", "off", "elsewhere", "pac", "autodetect", "partial"}[c]
}

// Check says whether the effective proxy sends apps to Kumo's port on
// this computer, and if not why, with the address in the way for
// Elsewhere (or the script's URL for PAC).
func (e Effective) Check(port int) (Cause, string) {
	ours := func(addr string) bool { return isOurs(addr, port) }
	if e.PAC != "" {
		return PAC, e.PAC
	}
	if e.AutoDetect {
		return AutoDetect, ""
	}
	if e.HTTP == "" && e.HTTPS == "" && e.SOCKS == "" {
		return Off, ""
	}
	for _, a := range []string{e.HTTP, e.HTTPS, e.SOCKS} {
		if a != "" && !ours(a) {
			return Elsewhere, a
		}
	}
	if ours(e.HTTP) && ours(e.HTTPS) {
		return Applied, ""
	}
	return Partial, ""
}

// isOurs reports whether addr is port on this computer.
func isOurs(addr string, port int) bool {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return loopback(host) && p == strconv.Itoa(port)
}

func loopback(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]'\"")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func join(host, port string) string {
	host, port = strings.TrimSpace(host), strings.TrimSpace(port)
	if host == "" || port == "" || port == "0" {
		return ""
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), port)
}

// parseScutil reads the top level of `scutil --proxy`'s dictionary.
func parseScutil(out string) Effective {
	e := Effective{Source: "scutil --proxy"}
	v := map[string]string{}
	depth := 0
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasSuffix(t, "{") {
			depth++
			if depth > 1 {
				continue
			}
		}
		if t == "}" {
			depth--
			continue
		}
		if depth != 1 {
			continue
		}
		if k, val, ok := strings.Cut(t, " : "); ok && !strings.HasSuffix(val, "{") {
			v[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
	}
	on := func(k string) bool { return v[k+"Enable"] == "1" }
	if on("HTTP") {
		e.HTTP = join(v["HTTPProxy"], v["HTTPPort"])
	}
	if on("HTTPS") {
		e.HTTPS = join(v["HTTPSProxy"], v["HTTPSPort"])
	}
	if on("SOCKS") {
		e.SOCKS = join(v["SOCKSProxy"], v["SOCKSPort"])
	}
	if v["ProxyAutoConfigEnable"] == "1" {
		e.PAC = v["ProxyAutoConfigURLString"]
		if e.PAC == "" {
			e.PAC = "(script)" // a PAC given as JavaScript, without a URL
		}
	}
	e.AutoDetect = v["ProxyAutoDiscoveryEnable"] == "1"
	return e
}

// WinINet's flags in the connection settings blob (byte 8).
const (
	flagManual = 0x02 // use ProxyServer
	// flagAutoURL and flagAutoDetect are in sysproxy_windows.go's terms:
	// 0x04 AutoConfigURL, 0x08 discovery.
	winAutoURL    = 0x04
	winAutoDetect = 0x08
)

// parseWinINet reads WinINet's settings: ProxyEnable, ProxyServer
// ("host:port", or "http=h:p;https=h:p;socks=h:p"), AutoConfigURL, and
// the default connection's blob, whose flags are what Windows applies
// when it is there.
func parseWinINet(enable bool, server, autoURL string, blob []byte) Effective {
	e := Effective{Source: "WinINet"}
	manual, pac, detect := enable, autoURL != "", false
	if len(blob) > 8 {
		manual = blob[8]&flagManual != 0
		pac = blob[8]&winAutoURL != 0 && autoURL != ""
		detect = blob[8]&winAutoDetect != 0
	}
	if manual {
		for _, part := range strings.Split(server, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			kind, addr, ok := strings.Cut(part, "=")
			if !ok {
				addr = part
				if e.HTTP == "" {
					e.HTTP, e.HTTPS = addr, addr
				}
				continue
			}
			switch strings.ToLower(kind) {
			case "http":
				e.HTTP = addr
			case "https":
				e.HTTPS = addr
			case "socks":
				e.SOCKS = addr
			}
		}
	}
	if pac {
		e.PAC = autoURL
	}
	e.AutoDetect = detect
	return e
}

// parseGnome reads GNOME's proxy settings, by "schema key" as gsettings
// prints them ('quoted' strings).
func parseGnome(get func(schema, key string) string) Effective {
	e := Effective{Source: "gsettings"}
	unq := func(s string) string { return strings.Trim(strings.TrimSpace(s), "'") }
	switch unq(get("org.gnome.system.proxy", "mode")) {
	case "manual":
		at := func(kind string) string {
			return join(unq(get("org.gnome.system.proxy."+kind, "host")), get("org.gnome.system.proxy."+kind, "port"))
		}
		e.HTTP, e.HTTPS, e.SOCKS = at("http"), at("https"), at("socks")
	case "auto":
		if url := unq(get("org.gnome.system.proxy", "autoconfig-url")); url != "" {
			e.PAC = url
		} else {
			e.AutoDetect = true
		}
	}
	return e
}

// parseEnv reads the proxy variables of an environment.
func parseEnv(getenv func(string) string) Effective {
	e := Effective{Source: "environment"}
	first := func(names ...string) string {
		for _, n := range names {
			if v := getenv(n); v != "" {
				return hostPort(v)
			}
		}
		return ""
	}
	all := first("all_proxy", "ALL_PROXY")
	e.HTTP = first("http_proxy", "HTTP_PROXY")
	e.HTTPS = first("https_proxy", "HTTPS_PROXY")
	if e.HTTP == "" {
		e.HTTP = all
	}
	if e.HTTPS == "" {
		e.HTTPS = all
	}
	return e
}

// hostPort is the host:port of a proxy URL such as http://127.0.0.1:7890/.
func hostPort(v string) string {
	if _, rest, ok := strings.Cut(v, "://"); ok {
		v = rest
	}
	v, _, _ = strings.Cut(v, "/")
	if i := strings.LastIndex(v, "@"); i >= 0 {
		v = v[i+1:]
	}
	return v
}

// OffWhereDead turns off what the state's proxies would point at a port
// of this computer that nothing listens on (alive says whether one does):
// a backup taken while another client, or a crashed Kumo, had set the
// proxy would otherwise restore a proxy that blocks every app. It
// reports whether it changed anything.
func (s State) OffWhereDead(alive func(port int) bool) (State, bool) {
	out := State{Values: make(map[string]string, len(s.Values))}
	for k, v := range s.Values {
		out.Values[k] = v
	}
	dead := func(host, port string) bool {
		p, err := strconv.Atoi(strings.Trim(strings.TrimSpace(port), "'"))
		return err == nil && p > 0 && loopback(host) && !alive(p)
	}
	deadAddr := func(addr string) bool {
		h, p, err := net.SplitHostPort(strings.TrimSpace(addr))
		return err == nil && dead(h, p)
	}
	deadURL := func(u string) bool { return u != "" && deadAddr(hostPortOrDefault(hostPort(u))) }
	changed := false
	for k, v := range s.Values {
		switch {
		// macOS: "service\t-getwebproxy" = "Yes\thost\tport".
		case strings.Contains(k, "\t-get"):
			parts := strings.Split(v, "\t")
			if len(parts) == 3 && parts[0] == "Yes" && dead(parts[1], parts[2]) {
				out.Values[k] = "No\t" + parts[1] + "\t" + parts[2]
				changed = true
			}
		case strings.HasSuffix(k, "\tpac"):
			enabled, url, _ := strings.Cut(v, "\t")
			if enabled == "Yes" && deadURL(url) {
				out.Values[k] = "No\t" + url
				changed = true
			}
		}
	}
	// Windows: ProxyEnable with ProxyServer, AutoConfigURL, and the flags.
	if srv, ok := s.Values["ProxyServer"]; ok && s.Values["ProxyEnable"] == "1" {
		e := parseWinINet(true, srv, "", nil)
		if (e.HTTP != "" && deadAddr(e.HTTP)) || (e.HTTPS != "" && deadAddr(e.HTTPS)) {
			out.Values["ProxyEnable"] = "0"
			out.Values[defaultConnectionKey] = clearFlag(out.Values[defaultConnectionKey], flagManual)
			changed = true
		}
	}
	if u, ok := s.Values["AutoConfigURL"]; ok && deadURL(u) {
		delete(out.Values, "AutoConfigURL")
		out.Values[defaultConnectionKey] = clearFlag(out.Values[defaultConnectionKey], winAutoURL)
		changed = true
	}
	// GNOME: mode 'manual' with the HTTP proxy at a dead port.
	if strings.Trim(s.Values["org.gnome.system.proxy mode"], "'") == "manual" &&
		dead(strings.Trim(s.Values["org.gnome.system.proxy.http host"], "'"), s.Values["org.gnome.system.proxy.http port"]) {
		out.Values["org.gnome.system.proxy mode"] = "'none'"
		changed = true
	}
	if out.Values[defaultConnectionKey] == "" {
		if _, had := s.Values[defaultConnectionKey]; !had {
			delete(out.Values, defaultConnectionKey)
		}
	}
	return out, changed
}

// defaultConnectionKey is the Windows blob's key in a State.
const defaultConnectionKey = "DefaultConnectionSettings"

func hostPortOrDefault(hp string) string {
	if _, _, err := net.SplitHostPort(hp); err == nil {
		return hp
	}
	return hp + ":80"
}

// clearFlag clears flag in byte 8 of a base64 WinINet blob.
func clearFlag(b64 string, flag byte) string {
	blob, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(blob) <= 8 {
		return b64
	}
	blob[8] &^= flag
	return base64.StdEncoding.EncodeToString(blob)
}
