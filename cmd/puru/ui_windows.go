//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// enableVirtualTerminal turns on ANSI escape processing for the classic
// Windows console (cmd.exe / PowerShell), which leaves it off by default.
// Windows Terminal already processes the sequences and redirected output is
// not a console handle, so failures are intentionally ignored.
func enableVirtualTerminal() {
	const enableVTOutput = 0x0004 // ENABLE_VIRTUAL_TERMINAL_PROCESSING
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	handle := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if ret, _, _ := getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode))); ret == 0 {
		return
	}
	_, _, _ = setConsoleMode.Call(uintptr(handle), uintptr(mode|enableVTOutput))
}
