package main

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"kumo/sysproxy"
)

// Whether the system proxy Kumo set is still in force. Another proxy app,
// a VPN client, a PAC script from the network, or the user can change it
// behind Kumo's back; Kumo reads the system's effective proxy after
// turning it on, every proxyEvery, on a change of network and on waking,
// and shows the tile and the tray amber with the cause when it no longer
// points at Kumo.

// proxyEvery is how often the effective proxy is read while it is on.
const proxyEvery = 15 * time.Second

// proxyState is the last reading.
type proxyState struct {
	known  bool // the system's settings could be read
	cause  sysproxy.Cause
	detail string
	source string
	at     time.Time
}

// stale says the proxy is on but not in force.
func (p proxyState) stale() bool { return p.known && p.cause != sysproxy.Applied }

// readProxy is sysproxy.Read; a variable so tests can stand in.
var readProxy = sysproxy.Read

// proxyApplied records, on the main thread, that the system proxy is now
// on at port (or off), and reads it back soon.
func (k *Kumo) proxyApplied(on bool, port int) {
	if !on {
		k.proxyWant.Store(0)
		k.proxyEff = proxyState{}
		return
	}
	k.proxyWant.Store(int64(port))
	// The system takes a moment to apply what it was told.
	time.AfterFunc(1500*time.Millisecond, k.checkProxy)
}

// checkProxy reads the effective proxy, off the main thread, and posts it.
func (k *Kumo) checkProxy() {
	port := int(k.proxyWant.Load())
	if port == 0 || !k.proxyChecking.CompareAndSwap(false, true) {
		return
	}
	defer k.proxyChecking.Store(false)
	e, err := readProxy()
	st := proxyState{known: err == nil, source: e.Source, at: time.Now()}
	if err == nil {
		st.cause, st.detail = e.Check(port)
	}
	k.post(func() {
		if int(k.proxyWant.Load()) != port || !k.proxyOn {
			return
		}
		was := k.proxyEff.stale()
		k.proxyEff = st
		if st.stale() && !was {
			k.say(tr("The system proxy no longer points at Kumo: ") + proxyCause(st))
		}
		k.refreshMenus()
	})
}

// watchProxy reads the proxy every proxyEvery while it is on, and at once
// when the network changes or the computer wakes (a tick that comes far
// later than due).
func (k *Kumo) watchProxy() {
	sig := netSignature()
	last := time.Now()
	for range time.Tick(proxyEvery / 3) {
		now := time.Now()
		woke := now.Sub(last) > proxyEvery
		last = now
		s := netSignature()
		changed := s != sig
		sig = s
		if k.proxyWant.Load() == 0 {
			continue
		}
		due := k.proxyTick.Add(1)%3 == 0
		if changed || woke {
			// The new network's settings (DHCP's WPAD, a VPN) come a moment
			// after its addresses.
			time.AfterFunc(2*time.Second, k.checkProxy)
		}
		if due || changed || woke {
			k.checkProxy()
		}
	}
}

// netSignature sums up the computer's network addresses, which change
// with the network.
func netSignature() string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var parts []string
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			parts = append(parts, i.Name+"="+a.String())
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, ",")
}

// proxyCause says in plain words why the system proxy is not Kumo's.
func proxyCause(st proxyState) string {
	switch st.cause {
	case sysproxy.Off:
		return tr("The system proxy was turned off, by another app or in the system's settings.")
	case sysproxy.Elsewhere:
		return fmt.Sprintf(tr("It now points at %s; another proxy or VPN app may have taken it over."), st.detail)
	case sysproxy.PAC:
		return fmt.Sprintf(tr("An automatic proxy configuration script (%s) decides before Kumo's proxy."), st.detail)
	case sysproxy.AutoDetect:
		return tr("Automatic proxy discovery is on and may send apps elsewhere.")
	case sysproxy.Partial:
		return tr("Only some kinds of traffic point at Kumo.")
	}
	return ""
}

// reapplyProxy points the system at Kumo again, keeping the backup of the
// user's own settings taken when it was first turned on.
func (k *Kumo) reapplyProxy() {
	if k.proxyBusy || !k.online {
		return
	}
	port, backup := k.listenPort(), k.backupFile()
	k.proxyBusy = true
	go func() {
		err := proxyOn(backup, port)
		k.post(func() {
			k.proxyBusy = false
			if err != nil {
				k.say(explainProxyError(err, port))
				return
			}
			k.proxyOn, k.proxyPort, k.proxyErr = true, port, ""
			k.proxyApplied(true, port)
			k.say(tr("System proxy applied again"))
			k.refreshMenus()
		})
	}()
}

