//go:build windows

package app

import "syscall"

func gatewaySysProcAttr() *syscall.SysProcAttr {
	return nil
}
