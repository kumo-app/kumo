package nodetest

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDownloadLimits(t *testing.T) {
	// Serves as the proxy too: a plain-http URL is fetched with its full
	// address from the proxy.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := make([]byte, 32<<10)
		for i := 0; i < 2000; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(time.Millisecond)
		}
	}))
	defer srv.Close()
	proxy := strings.TrimPrefix(srv.URL, "http://")
	var calls int
	s, err := Download(context.Background(), proxy, "http://speed.test/__down?bytes=1", 1<<20, 5*time.Second, func(Speed) { calls++ })
	if err != nil || s.Bytes < 1<<20 || s.Bytes > 1<<20+64<<10 || s.Mbps() <= 0 {
		t.Fatalf("size cap: %+v %v", s, err)
	}
	s, err = Download(context.Background(), proxy, "http://speed.test/", 1<<40, 300*time.Millisecond, nil)
	if err != nil || s.Duration < 300*time.Millisecond || s.Duration > time.Second {
		t.Fatalf("time cap: %+v %v", s, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	if _, err := Download(ctx, proxy, "http://speed.test/", 1<<40, 10*time.Second, nil); err == nil {
		t.Fatal("cancel not reported")
	}
}

// fakeSocks is a SOCKS5 server whose UDP relay forwards to the real
// destination when udp is true, and refuses ASSOCIATE otherwise; silent
// drops every datagram.
func fakeSocks(t *testing.T, udp, silent bool) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				b := make([]byte, 10)
				io.ReadFull(c, b[:3])
				c.Write([]byte{5, 0})
				io.ReadFull(c, b[:10])
				if !udp {
					c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				relay, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
				defer relay.Close()
				rep := []byte{5, 0, 0, 1, 0, 0, 0, 0}
				rep = binary.BigEndian.AppendUint16(rep, uint16(relay.LocalAddr().(*net.UDPAddr).Port))
				c.Write(rep)
				go func() {
					buf := make([]byte, 2048)
					for {
						n, from, err := relay.ReadFromUDP(buf)
						if err != nil {
							return
						}
						if silent {
							continue
						}
						body, _ := stripUDPHeader(buf[:n])
						dst := &net.UDPAddr{IP: net.IP(buf[4:8]), Port: int(binary.BigEndian.Uint16(buf[8:10]))}
						up, _ := net.DialUDP("udp", nil, dst)
						up.Write(body)
						up.SetReadDeadline(time.Now().Add(time.Second))
						ans := make([]byte, 2048)
						m, err := up.Read(ans)
						up.Close()
						if err == nil {
							relay.WriteToUDP(append(slicesClone(buf[:10]), ans[:m]...), from)
						}
					}
				}()
				io.Copy(io.Discard, c)
			}()
		}
	}()
	return l.Addr().String()
}

func slicesClone(b []byte) []byte { return append([]byte(nil), b...) }

// fakeDNS answers every query with its ID and the answer bit.
func fakeDNS(t *testing.T) string {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		b := make([]byte, 512)
		for {
			n, from, err := pc.ReadFromUDP(b)
			if err != nil {
				return
			}
			b[2] |= 0x80
			pc.WriteToUDP(b[:n], from)
		}
	}()
	return pc.LocalAddr().String()
}

func TestCheckUDP(t *testing.T) {
	dns := fakeDNS(t)
	ctx := context.Background()
	if r, err := CheckUDP(ctx, fakeSocks(t, true, false), dns, "example.com", 2*time.Second); r != UDPSupported {
		t.Fatalf("relaying proxy: %v %v", r, err)
	}
	if r, _ := CheckUDP(ctx, fakeSocks(t, false, false), dns, "example.com", 2*time.Second); r != UDPUnsupported {
		t.Fatalf("refusing proxy: %v", r)
	}
	if r, _ := CheckUDP(ctx, fakeSocks(t, true, true), dns, "example.com", 1500*time.Millisecond); r != UDPUnsupported {
		t.Fatalf("silent proxy: %v", r)
	}
	if r, _ := CheckUDP(ctx, "127.0.0.1:1", dns, "example.com", time.Second); r != UDPUnknown {
		t.Fatalf("no proxy: %v", r)
	}
}

func TestDNSQuery(t *testing.T) {
	q := dnsQuery(0x1234, "example.com")
	want := "\x12\x34\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00\x07example\x03com\x00\x00\x01\x00\x01"
	if string(q) != want {
		t.Fatalf("%q", q)
	}
}
