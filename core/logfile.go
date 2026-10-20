package core

import (
	"os"
	"sync"
)

// LogLimit is how large the core's log grows before it starts over. The
// previous part is kept beside it as core.log.1, so the log takes at most
// twice this. A busy core logging every connection writes about 2 MB a
// minute at the info level.
const LogLimit = 5 << 20

// capFile is a log file that starts over past its limit, keeping the
// part before as path.1.
type capFile struct {
	mu    sync.Mutex
	path  string
	limit int64
	f     *os.File
	n     int64
}

func openCapped(path string, limit int64) (*capFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return &capFile{path: path, limit: limit, f: f}, nil
}

func (c *capFile) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f == nil {
		return len(p), nil
	}
	if c.n > 0 && c.n+int64(len(p)) > c.limit {
		c.rotate()
	}
	n, err := c.f.Write(p)
	c.n += int64(n)
	return n, err
}

// rotate moves the log to path.1 and starts a new one. Where the move
// fails (Windows, while another program has the log open) it empties the
// file in place.
func (c *capFile) rotate() {
	c.f.Close()
	if err := os.Rename(c.path, c.path+".1"); err == nil {
		if f, err := os.OpenFile(c.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
			c.f, c.n = f, 0
			return
		}
	}
	f, err := os.OpenFile(c.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		c.f = nil // nowhere to write; the core runs on without a log
		return
	}
	c.f, c.n = f, 0
}

func (c *capFile) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f == nil {
		return nil
	}
	err := c.f.Close()
	c.f = nil
	return err
}
