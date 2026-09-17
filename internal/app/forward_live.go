package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

)

var forwardProxies sync.Map // id -> *forwardProxy

type forwardProxy struct {
	cancel context.CancelFunc
}

func (a *App) startForwardProxy(sandbox, hostPort, guestPort string) error {
	if a.Docker == nil {
		return fmt.Errorf("docker unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, sandbox)
	if err != nil {
		return err
	}
	ip, err := a.Docker.ContainerIP(ctx, string(info.ID), info.Network)
	if err != nil {
		return err
	}
	guest, err := strconv.Atoi(strings.TrimSpace(guestPort))
	if err != nil || guest <= 0 {
		return fmt.Errorf("invalid guest port %q", guestPort)
	}
	host, err := strconv.Atoi(strings.TrimSpace(hostPort))
	if err != nil || host <= 0 {
		return fmt.Errorf("invalid host port %q", hostPort)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", host))
	if err != nil {
		return err
	}
	proxyCtx, proxyCancel := context.WithCancel(context.Background())
	id := sandbox + ":" + hostPort
	if old, ok := forwardProxies.Load(id); ok {
		if p, ok := old.(*forwardProxy); ok && p.cancel != nil {
			p.cancel()
		}
	}
	forwardProxies.Store(id, &forwardProxy{cancel: proxyCancel})
	target := fmt.Sprintf("%s:%d", ip, guest)
	go func() {
		defer ln.Close()
		for {
			select {
			case <-proxyCtx.Done():
				return
			default:
			}
			conn, err := ln.Accept()
			if err != nil {
				if proxyCtx.Err() != nil {
					return
				}
				continue
			}
			go pipeForward(proxyCtx, conn, target)
		}
	}()
	fmt.Printf("forward: 127.0.0.1:%d → %s (%s)\n", host, target, sandbox)
	return nil
}

func pipeForward(ctx context.Context, client net.Conn, target string) {
	defer client.Close()
	up, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(up, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, up)
		done <- struct{}{}
	}()
	select {
	case <-ctx.Done():
	case <-done:
	}
}

func (a *App) stopForwardProxy(id string) {
	if old, ok := forwardProxies.Load(id); ok {
		if p, ok := old.(*forwardProxy); ok && p.cancel != nil {
			p.cancel()
		}
		forwardProxies.Delete(id)
	}
}
