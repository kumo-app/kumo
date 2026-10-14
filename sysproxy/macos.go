package sysproxy

import (
	"strconv"
	"strings"
	"sync"
)

// networkSetup runs networksetup with args, returning its output.
type networkSetup func(args ...string) (string, error)

// macProxy drives macOS's proxy settings through networksetup. It has no
// build constraint so that its logic is tested everywhere against a fake.
// Each network service is handled in its own goroutine: a Mac with VPN,
// Thunderbolt Bridge and iPhone USB services has many, and networksetup
// takes about 0.1 s a call.
type macProxy struct{ ns networkSetup }

// macKinds are the proxies Kumo sets, by their get, set and state verbs.
var macKinds = []struct{ get, set, state string }{
	{"-getwebproxy", "-setwebproxy", "-setwebproxystate"},
	{"-getsecurewebproxy", "-setsecurewebproxy", "-setsecurewebproxystate"},
	{"-getsocksfirewallproxy", "-setsocksfirewallproxy", "-setsocksfirewallproxystate"},
}

func (m macProxy) services() ([]string, error) {
	out, err := m.ns("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var list []string
	for i, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// The first line explains the asterisk of disabled services.
		if i == 0 || line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		list = append(list, line)
	}
	return list, nil
}

// field reads "Name: value" from networksetup's output.
func field(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == name {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// each runs fn for every service at once and returns the first error.
func each(list []string, fn func(svc string) error) error {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	for _, svc := range list {
		wg.Go(func() {
			if err := fn(svc); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return first
}

func (m macProxy) snapshot() (State, error) {
	s := State{Values: map[string]string{}}
	list, err := m.services()
	if err != nil {
		return s, err
	}
	s.Values["services"] = strings.Join(list, "\n")
	var mu sync.Mutex
	put := func(k, v string) { mu.Lock(); s.Values[k] = v; mu.Unlock() }
	err = each(list, func(svc string) error {
		for _, k := range macKinds {
			out, err := m.ns(k.get, svc)
			if err != nil {
				return err
			}
			put(svc+"\t"+k.get, field(out, "Enabled")+"\t"+field(out, "Server")+"\t"+field(out, "Port"))
		}
		out, err := m.ns("-getproxybypassdomains", svc)
		if err != nil {
			return err
		}
		var domains []string
		if !strings.Contains(out, "aren't any") {
			domains = strings.Fields(out)
		}
		put(svc+"\tbypass", strings.Join(domains, " "))
		if out, err = m.ns("-getautoproxyurl", svc); err != nil {
			return err
		}
		url := field(out, "URL")
		if url == "(null)" {
			url = ""
		}
		put(svc+"\tpac", field(out, "Enabled")+"\t"+url)
		if out, err = m.ns("-getproxyautodiscovery", svc); err != nil {
			return err
		}
		put(svc+"\twpad", field(out, "Auto Proxy Discovery"))
		return nil
	})
	return s, err
}

// set points every service at host:port, and turns off automatic proxy
// configuration (PAC) and discovery (WPAD), which would otherwise decide
// first and send apps around Kumo.
func (m macProxy) set(host string, port int) error {
	list, err := m.services()
	if err != nil {
		return err
	}
	p := strconv.Itoa(port)
	return each(list, func(svc string) error {
		for _, args := range [][]string{
			{"-setwebproxy", svc, host, p},
			{"-setsecurewebproxy", svc, host, p},
			{"-setsocksfirewallproxy", svc, host, p},
			append([]string{"-setproxybypassdomains", svc}, Bypass...),
			{"-setautoproxystate", svc, "off"},
			{"-setproxyautodiscovery", svc, "off"},
		} {
			if _, err := m.ns(args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// restore puts back every setting of the snapshot, servers included: one
// that was empty is emptied again, so Kumo's address does not linger.
func (m macProxy) restore(s State) error {
	var list []string
	for _, svc := range strings.Split(s.Values["services"], "\n") {
		if svc != "" {
			list = append(list, svc)
		}
	}
	return each(list, func(svc string) error {
		var first error
		keep := func(_ string, err error) {
			if err != nil && first == nil {
				first = err
			}
		}
		for _, k := range macKinds {
			parts := strings.Split(s.Values[svc+"\t"+k.get], "\t")
			if len(parts) != 3 {
				continue
			}
			enabled, server, port := parts[0], parts[1], parts[2]
			if server != "" && port != "" && port != "0" {
				keep(m.ns(k.set, svc, server, port))
			} else {
				// Best effort: networksetup may refuse an empty server.
				m.ns(k.set, svc, "", "0")
			}
			keep(m.ns(k.state, svc, onOff(enabled == "Yes")))
		}
		bypass := strings.Fields(s.Values[svc+"\tbypass"])
		if len(bypass) == 0 {
			bypass = []string{"Empty"}
		}
		keep(m.ns(append([]string{"-setproxybypassdomains", svc}, bypass...)...))
		if pac, ok := s.Values[svc+"\tpac"]; ok {
			enabled, url, _ := strings.Cut(pac, "\t")
			if url != "" {
				keep(m.ns("-setautoproxyurl", svc, url))
			}
			keep(m.ns("-setautoproxystate", svc, onOff(enabled == "Yes")))
		}
		if wpad, ok := s.Values[svc+"\twpad"]; ok {
			keep(m.ns("-setproxyautodiscovery", svc, onOff(wpad == "On")))
		}
		return first
	})
}

// clearOurs turns off the proxies that point at host:port, and only those.
func (m macProxy) clearOurs(host string, port int) error {
	list, err := m.services()
	if err != nil {
		return err
	}
	return each(list, func(svc string) error {
		for _, k := range macKinds {
			out, err := m.ns(k.get, svc)
			if err != nil {
				return err
			}
			if field(out, "Server") == host && field(out, "Port") == strconv.Itoa(port) {
				if _, err := m.ns(k.state, svc, "off"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
