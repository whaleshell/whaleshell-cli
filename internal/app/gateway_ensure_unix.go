//go:build unix

package app

import "syscall"

func gatewaySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
