//go:build (darwin || windows) && !headless && windows

package main

import "syscall"

func newSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
