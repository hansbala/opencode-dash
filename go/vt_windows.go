//go:build windows

package main

import "golang.org/x/sys/windows"

const enableVirtualTerminalProcessing = 0x0004

func enableVT() {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Stdout, &mode); err == nil {
		_ = windows.SetConsoleMode(windows.Stdout, mode|enableVirtualTerminalProcessing)
	}
}
