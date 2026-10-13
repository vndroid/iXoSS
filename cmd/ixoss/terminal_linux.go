package main

import (
	"os"
	"syscall"
	"unsafe"
)

func isTerminal(output *os.File) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, output.Fd(), syscall.TCGETS,
		uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
