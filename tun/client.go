//go:build unix

package tun

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// Timeouts of the client, variables for tests.
var (
	answerWait = 10 * time.Second
	stopWait   = 4 * time.Second
	poll       = 100 * time.Millisecond
)

// RunClient stands in for the core when the helper runs it as root (see
// helperScript). It takes the core's arguments:
//
//	-v                 print the core's version
//	-t -d dir -f file  test a configuration (run as the user)
//	-d dir -f file     run: hand the configuration to the helper, copy the
//	                   core's log to stdout until the core stops, and stop it
//	                   on SIGINT or SIGTERM
//
// It returns the exit code.
func RunClient(root string, args []string) int {
	return runClient(helperPaths{root: root, uid: os.Getuid()}, args, os.Stdout, os.Stderr)
}

func runClient(p helperPaths, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-v" || args[0] == "-t") {
		cmd := exec.Command(p.core(), args...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return exit.ExitCode()
			}
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	file := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-f" {
			file = args[i+1]
		}
	}
	config, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(stderr, "kumo tun client:", err)
		return 2
	}
	port := controllerPort(config)
	if port == 0 {
		fmt.Fprintln(stderr, "kumo tun client: the configuration names no controller on 127.0.0.1")
		return 2
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	return session(p, config, port, sig, stdout, stderr)
}

// controllerPort is the port of a loopback external-controller.
func controllerPort(config []byte) int {
	var m struct {
		Controller string `yaml:"external-controller"`
	}
	if yaml.Unmarshal(config, &m) != nil {
		return 0
	}
	host, port, err := net.SplitHostPort(m.Controller)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readState(p helperPaths) (word, nonce, code string) {
	data, _ := os.ReadFile(p.state())
	f := strings.Fields(string(data))
	for len(f) < 3 {
		f = append(f, "")
	}
	return f[0], f[1], f[2]
}

// session runs one core: request, wait for the helper, follow the log.
func session(p helperPaths, config []byte, port int, sig <-chan os.Signal, stdout, stderr io.Writer) int {
	b := make([]byte, 8)
	rand.Read(b)
	nonce := hex.EncodeToString(b)
	if err := writeAtomic(p.config(), config); err != nil {
		fmt.Fprintln(stderr, "kumo tun client:", err)
		return 2
	}
	req := fmt.Sprintf("start %d %s %d\n", os.Getpid(), nonce, port)
	if err := writeAtomic(p.request(), []byte(req)); err != nil {
		fmt.Fprintln(stderr, "kumo tun client:", err)
		return 2
	}
	stop := func() int {
		_ = writeAtomic(p.request(), []byte("stop "+nonce+"\n"))
		for end := time.Now().Add(stopWait); time.Now().Before(end); time.Sleep(poll) {
			if w, n, _ := readState(p); w == "stopped" && n == nonce {
				break
			}
		}
		return 0
	}
	// Wait for the helper to answer, asking again halfway in case launchd
	// missed the change.
	start, asked := time.Now(), false
	for {
		w, n, code := readState(p)
		if n == nonce && w == "running" {
			break
		}
		if n == nonce && w == "stopped" {
			fmt.Fprintln(stderr, "kumo tun client: the core did not start:", code)
			copyLog(p, stdout)
			return 1
		}
		select {
		case <-sig:
			return stop()
		case <-time.After(poll):
		}
		if !asked && time.Since(start) > answerWait/2 {
			asked = true
			_ = os.WriteFile(p.request(), []byte(req), 0o600)
		}
		if time.Since(start) > answerWait {
			fmt.Fprintln(stderr, "kumo tun client: the Enhanced Mode helper did not answer; turn Enhanced Mode off and on to set it up again")
			_ = writeAtomic(p.request(), []byte("stop "+nonce+"\n"))
			return 1
		}
	}
	log, _ := os.Open(p.log())
	if log != nil {
		defer log.Close()
	}
	for {
		if log != nil {
			io.Copy(stdout, log)
		}
		if w, n, code := readState(p); n == nonce && w == "stopped" {
			if log != nil {
				io.Copy(stdout, log)
			}
			c, _ := strconv.Atoi(code)
			return max(c, 1) // the core ended on its own
		}
		select {
		case <-sig:
			return stop()
		case <-time.After(poll * 3):
		}
	}
}

func copyLog(p helperPaths, w io.Writer) {
	if f, err := os.Open(p.log()); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fmt.Fprintln(w, sc.Text())
		}
		f.Close()
	}
}
