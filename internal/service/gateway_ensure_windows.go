//go:build windows

package service

import "syscall"

func gatewaySysProcAttr() *syscall.SysProcAttr {
	return nil
}
