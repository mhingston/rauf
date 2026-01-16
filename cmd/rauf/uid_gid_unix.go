//go:build !windows

package main

import "os"

func hostUIDGID() (int, int) {
	return os.Getuid(), os.Getgid()
}
