//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "word4win is a Windows application; build it with GOOS=windows (or use word4win-cli)")
	os.Exit(1)
}
