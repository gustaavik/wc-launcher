//go:build !windows

package gamesvc

import "syscall"

// sysProcAttr is Windows-only: elsewhere the child needs nothing special.
func sysProcAttr() *syscall.SysProcAttr { return nil }
