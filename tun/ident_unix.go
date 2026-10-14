//go:build unix

package tun

import (
	"fmt"
	"os"
	"syscall"
)

func identity(fi os.FileInfo) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
	}
	return ""
}
