//go:build windows

package main

func hostUIDGID() (int, int) {
	return -1, -1
}
