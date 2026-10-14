// Package nodetest measures one node through Kumo's test listener (see
// core.TestListener): download speed over HTTP, and whether UDP passes,
// with a DNS query over SOCKS5 UDP ASSOCIATE.
package nodetest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// SpeedURL is Cloudflare's speed test download; bytes= sets its size.
const SpeedURL = "https://speed.cloudflare.com/__down?bytes="

// Speed is a download's result.
type Speed struct {
	Bytes    int64
	Duration time.Duration
}

// Mbps is the speed in megabits per second.
func (s Speed) Mbps() float64 {
	if s.Duration <= 0 {
		return 0
	}
	return float64(s.Bytes) * 8 / s.Duration.Seconds() / 1e6
}

// Download fetches rawURL through the HTTP proxy at proxyAddr for at most
// limit bytes or dur, whichever comes first, calling progress as it goes.
// Ending at the time or size limit is a result, not an error; ctx ending
// is canceled.
func Download(ctx context.Context, proxyAddr, rawURL string, limit int64, dur time.Duration, progress func(Speed)) (Speed, error) {
	pu, _ := url.Parse("http://" + proxyAddr)
	tr := &http.Transport{Proxy: http.ProxyURL(pu), DisableCompression: true, ResponseHeaderTimeout: 10 * time.Second}
	defer tr.CloseIdleConnections()
	dctx, cancel := context.WithTimeout(ctx, dur+10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, "GET", rawURL, nil)
	if err != nil {
		return Speed{}, err
	}
	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return Speed{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Speed{}, fmt.Errorf("download: %s", res.Status)
	}
	// The clock starts with the first byte, so the handshakes do not count.
	buf := make([]byte, 64<<10)
	var s Speed
	var start time.Time
	last := time.Time{}
	for s.Bytes < limit {
		n, err := res.Body.Read(buf)
		if start.IsZero() && n > 0 {
			start = time.Now()
		}
		s.Bytes += int64(n)
		if !start.IsZero() {
			s.Duration = time.Since(start)
		}
		if progress != nil && s.Duration > 500*time.Millisecond && time.Since(last) > 200*time.Millisecond {
			progress(s)
			last = time.Now()
		}
		if s.Duration >= dur {
			break
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return s, ctx.Err()
			}
			if s.Bytes > 0 {
				break
			}
			return s, err
		}
	}
	return s, nil
}

// UDP is what a UDP check found.
type UDP int

const (
	// UDPUnknown: the check could not be made.
	UDPUnknown UDP = iota
	UDPSupported
	UDPUnsupported
)

// CheckUDP sends a DNS query for dnsName to resolver (host:port) through
// the SOCKS5 proxy at socksAddr. An answer means UDP passes the node; a
// refused association or silence until timeout means it does not; a
// proxy that cannot be reached means unknown.
func CheckUDP(ctx context.Context, socksAddr, resolver, dnsName string, timeout time.Duration) (UDP, error) {
	var d net.Dialer
	ctl, err := d.DialContext(ctx, "tcp", socksAddr)
	if err != nil {
		return UDPUnknown, err
	}
	defer ctl.Close()
	deadline := time.Now().Add(timeout)
	ctl.SetDeadline(deadline)
	// Greeting: no authentication.
	if _, err := ctl.Write([]byte{5, 1, 0}); err != nil {
		return UDPUnknown, err
	}
	var rep [2]byte
	if _, err := io.ReadFull(ctl, rep[:]); err != nil || rep[0] != 5 || rep[1] != 0 {
		return UDPUnknown, errors.New("socks5: no greeting")
	}
	// UDP ASSOCIATE from any address.
	if _, err := ctl.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return UDPUnknown, err
	}
	relay, code, err := readReply(ctl)
	if err != nil {
		return UDPUnknown, err
	}
	if code != 0 {
		return UDPUnsupported, fmt.Errorf("socks5: UDP ASSOCIATE refused (%d)", code)
	}
	if relay.IP.IsUnspecified() {
		relay.IP = ctl.RemoteAddr().(*net.TCPAddr).IP
	}
	pc, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		return UDPUnknown, err
	}
	defer pc.Close()
	host, portStr, err := net.SplitHostPort(resolver)
	if err != nil {
		return UDPUnknown, err
	}
	port, _ := strconv.Atoi(portStr)
	id := uint16(time.Now().UnixNano())
	pkt := socksUDPHeader(host, port)
	pkt = append(pkt, dnsQuery(id, dnsName)...)
	buf := make([]byte, 2048)
	// A few tries: the first packets may be lost while the node connects.
	for try := 0; time.Now().Before(deadline); try++ {
		if _, err := pc.Write(pkt); err != nil {
			return UDPUnknown, err
		}
		wait := min(time.Until(deadline), time.Second<<min(try, 2))
		pc.SetReadDeadline(time.Now().Add(wait))
		for {
			n, err := pc.Read(buf)
			if err != nil {
				break
			}
			if body, ok := stripUDPHeader(buf[:n]); ok && len(body) >= 12 && binary.BigEndian.Uint16(body) == id && body[2]&0x80 != 0 {
				return UDPSupported, nil
			}
		}
		if ctx.Err() != nil {
			return UDPUnknown, ctx.Err()
		}
	}
	return UDPUnsupported, errors.New("no DNS answer over UDP")
}

func readReply(r io.Reader) (*net.UDPAddr, byte, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, 0, err
	}
	if h[0] != 5 {
		return nil, 0, errors.New("socks5: bad reply")
	}
	var ip net.IP
	switch h[3] {
	case 1:
		ip = make(net.IP, 4)
	case 4:
		ip = make(net.IP, 16)
	case 3:
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return nil, 0, err
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(r, name); err != nil {
			return nil, 0, err
		}
		ip = net.IPv4zero // resolved below as the proxy's address
	default:
		return nil, 0, errors.New("socks5: bad address type")
	}
	if h[3] != 3 {
		if _, err := io.ReadFull(r, ip); err != nil {
			return nil, 0, err
		}
	}
	var p [2]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return nil, 0, err
	}
	return &net.UDPAddr{IP: ip, Port: int(binary.BigEndian.Uint16(p[:]))}, h[1], nil
}

func socksUDPHeader(host string, port int) []byte {
	b := []byte{0, 0, 0}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		b = append(b, 1)
		b = append(b, ip.To4()...)
	} else if ip != nil {
		b = append(b, 4)
		b = append(b, ip.To16()...)
	} else {
		b = append(b, 3, byte(len(host)))
		b = append(b, host...)
	}
	return binary.BigEndian.AppendUint16(b, uint16(port))
}

func stripUDPHeader(b []byte) ([]byte, bool) {
	if len(b) < 4 || b[2] != 0 {
		return nil, false
	}
	n := 4
	switch b[3] {
	case 1:
		n += 4
	case 4:
		n += 16
	case 3:
		if len(b) < 5 {
			return nil, false
		}
		n += 1 + int(b[4])
	default:
		return nil, false
	}
	n += 2
	if len(b) < n {
		return nil, false
	}
	return b[n:], true
}

// dnsQuery is a query for name's A record.
func dnsQuery(id uint16, name string) []byte {
	b := binary.BigEndian.AppendUint16(nil, id)
	b = append(b, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0) // RD; one question
	for _, l := range splitLabels(name) {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0, 0, 1, 0, 1)
}

func splitLabels(name string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i > start {
				out = append(out, name[start:i])
			}
			start = i + 1
		}
	}
	return out
}
