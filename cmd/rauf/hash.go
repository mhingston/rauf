package main

import (
	"crypto/sha256"
	"fmt"
)

func fileHashFromString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum)
}
