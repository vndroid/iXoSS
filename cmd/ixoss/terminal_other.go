//go:build !linux

package main

import "os"

// Linux is the supported runtime platform. Other targets use plain output.
func isTerminal(output *os.File) bool { return false }
