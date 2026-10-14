package tun

import "os"

func identity(os.FileInfo) string { return "" }
