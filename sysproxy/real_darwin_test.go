package sysproxy

import (
	"os"
	"strings"
	"testing"
)

// TestRealNetworksetupRestore runs set and restore against the real
// networksetup (CI: KUMO_REAL_NETWORKSETUP=1, as root). Services with no
// proxy server must come back without Kumo's address and with the proxy off.
func TestRealNetworksetupRestore(t *testing.T) {
	if os.Getenv("KUMO_REAL_NETWORKSETUP") == "" {
		t.Skip("KUMO_REAL_NETWORKSETUP not set")
	}
	st, err := mac.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("before: %q", st.Values)
	if err := mac.set("127.0.0.1", 7890); err != nil {
		t.Fatal(err)
	}
	if out, err := runNetworksetup("-setwebproxy", "Kumo-probe-does-not-exist", "", "0"); err != nil {
		t.Logf("empty server on a missing service: %v %s", err, out)
	}
	if err := mac.restore(st); err != nil {
		t.Fatalf("restore: %v", err)
	}
	after, err := mac.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after: %q", after.Values)
	for k, v := range after.Values {
		if strings.Contains(v, "127.0.0.1\t7890") {
			t.Errorf("%s still points at Kumo: %q (was %q)", k, v, st.Values[k])
		}
		if strings.HasPrefix(v, "Yes") && !strings.HasPrefix(st.Values[k], "Yes") {
			t.Errorf("%s left on: %q", k, v)
		}
	}
}
