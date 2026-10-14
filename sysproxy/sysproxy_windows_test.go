package sysproxy

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestWithoutAuto(t *testing.T) {
	blob := []byte{0x46, 0, 0, 0, 5, 0, 0, 0, 0x0f, 0, 0, 0}
	out := withoutAuto(blob)
	if out[8] != 0x03 || blob[8] != 0x0f {
		t.Fatalf("flags %#x (input %#x)", out[8], blob[8])
	}
	if got := withoutAuto([]byte{1, 2}); len(got) != 2 {
		t.Fatal("short blob changed")
	}
}

// TestSetRestore changes this user's WinINet settings and puts them back;
// CI runs it on a throwaway Windows runner.
func TestSetRestore(t *testing.T) {
	k, err := open(internetSettings, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		t.Skip(err)
	}
	defer k.Close()
	orig, err := snapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer restore(orig)
	// The user's own setup: a PAC file.
	k.SetStringValue("AutoConfigURL", "http://corp/proxy.pac")
	k.SetDWordValue("ProxyEnable", 0)
	before, _ := snapshot()
	if err := set("127.0.0.1", 7890); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.GetStringValue("AutoConfigURL"); err == nil {
		t.Fatal("PAC left in force")
	}
	if v, _, _ := k.GetStringValue("ProxyServer"); v != "127.0.0.1:7890" {
		t.Fatalf("ProxyServer %q", v)
	}
	if err := restore(before); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := k.GetStringValue("AutoConfigURL"); v != "http://corp/proxy.pac" {
		t.Fatalf("PAC not restored: %q", v)
	}
	if err := set("127.0.0.1", 7890); err != nil {
		t.Fatal(err)
	}
	if err := clearOurs("127.0.0.1", 7890); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := k.GetIntegerValue("ProxyEnable"); v != 0 {
		t.Fatal("Kumo's proxy left on")
	}
	k.SetStringValue("ProxyServer", "corp:3128")
	k.SetDWordValue("ProxyEnable", 1)
	clearOurs("127.0.0.1", 7890)
	if v, _, _ := k.GetIntegerValue("ProxyEnable"); v != 1 {
		t.Fatal("the user's own proxy turned off")
	}
}
