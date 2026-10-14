package sysproxy

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	internetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	connections      = internetSettings + `\Connections`
	// defaultConnection holds WinINet's settings of the default connection
	// as a blob whose flags (byte 8) include automatic configuration.
	defaultConnection = "DefaultConnectionSettings"
	flagAutoURL       = 0x04 // use AutoConfigURL (PAC)
	flagAutoDetect    = 0x08 // discover a PAC (WPAD)
)

func open(path string, access uint32) (registry.Key, error) {
	return registry.OpenKey(registry.CURRENT_USER, path, access)
}

func snapshot() (State, error) {
	s := State{Values: map[string]string{}}
	k, err := open(internetSettings, registry.QUERY_VALUE)
	if err != nil {
		return s, err
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		s.Values["ProxyEnable"] = strconv.FormatUint(v, 10)
	}
	for _, name := range []string{"ProxyServer", "ProxyOverride", "AutoConfigURL"} {
		if v, _, err := k.GetStringValue(name); err == nil {
			s.Values[name] = v
		}
	}
	if c, err := open(connections, registry.QUERY_VALUE); err == nil {
		if blob, _, err := c.GetBinaryValue(defaultConnection); err == nil {
			s.Values[defaultConnection] = base64.StdEncoding.EncodeToString(blob)
		}
		c.Close()
	}
	return s, nil
}

func restore(s State) error {
	k, err := open(internetSettings, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	var errs []error
	for _, name := range []string{"ProxyServer", "ProxyOverride", "AutoConfigURL"} {
		if v, ok := s.Values[name]; ok {
			errs = append(errs, k.SetStringValue(name, v))
		} else if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	on, _ := strconv.ParseUint(s.Values["ProxyEnable"], 10, 32)
	errs = append(errs, k.SetDWordValue("ProxyEnable", uint32(on)))
	if b64, ok := s.Values[defaultConnection]; ok {
		if blob, err := base64.StdEncoding.DecodeString(b64); err == nil {
			errs = append(errs, writeConnection(blob))
		}
	}
	refresh()
	return errors.Join(errs...)
}

func writeConnection(blob []byte) error {
	c, err := open(connections, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.SetBinaryValue(defaultConnection, blob)
}

// withoutAuto is the connection blob with PAC and WPAD turned off.
func withoutAuto(blob []byte) []byte {
	if len(blob) <= 8 {
		return blob
	}
	out := append([]byte(nil), blob...)
	out[8] &^= flagAutoURL | flagAutoDetect
	return out
}

func set(host string, port int) error {
	k, err := open(internetSettings, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("ProxyServer", fmt.Sprintf("%s:%d", host, port)); err != nil {
		return err
	}
	bypass := strings.Join(append(append([]string{}, Bypass...), "<local>"), ";")
	if err := k.SetStringValue("ProxyOverride", bypass); err != nil {
		return err
	}
	// A PAC script or WPAD would decide before the proxy and send apps
	// around Kumo; the backup puts them back.
	if err := k.DeleteValue("AutoConfigURL"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	if c, err := open(connections, registry.QUERY_VALUE); err == nil {
		blob, _, err := c.GetBinaryValue(defaultConnection)
		c.Close()
		if err == nil {
			if err := writeConnection(withoutAuto(blob)); err != nil {
				return err
			}
		}
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	refresh()
	return nil
}

func clearOurs(host string, port int) error {
	k, err := open(internetSettings, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if v, _, err := k.GetStringValue("ProxyServer"); err != nil || v != fmt.Sprintf("%s:%d", host, port) {
		return nil // not Kumo's
	}
	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return err
	}
	refresh()
	return nil
}

// refresh tells WinINet, and the browsers that follow it, that the
// settings changed.
func refresh() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	set := wininet.NewProc("InternetSetOptionW")
	const settingsChanged, refreshOption = 39, 37
	set.Call(0, settingsChanged, 0, 0)
	set.Call(0, refreshOption, 0, 0)
}
