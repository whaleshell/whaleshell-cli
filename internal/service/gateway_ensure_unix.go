//go:build unix

package service

import "syscall"

func gatewaySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
