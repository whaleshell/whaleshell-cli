package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/whaleshell/slogx"
	"github.com/whaleshell/whaleshell-cli/internal/storage/gwconfig"
	"github.com/whaleshell/whaleshell-core/defaults"
	"github.com/whaleshell/whaleshell-driver/driver"
	"github.com/whaleshell/whaleshell-driver/mounts"
	"github.com/whaleshell/whaleshell-driver/sidecar"
	"github.com/whaleshell/whaleshell-sdk/gatewayclient"
	"golang.org/x/crypto/ssh"
)

// SandboxStop stops a sandbox without deleting network/volume.
func (a *App) SandboxStop(name string) error {
	const op = "cli.sandbox.stop"
	log := a.op(op, slog.String("sandbox", name))
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("sandbox stop: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	log.Info("stopping sandbox")
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		log.Error("failed to inspect sandbox", slogx.Err(err))
		return err
	}
	if err := a.Sandboxes.Driver.Stop(ctx, info.ID); err != nil {
		log.Error("failed to stop sandbox", slogx.Err(err))
		return err
	}
	a.touchGateway(ctx, info.Name, string(info.ID), info.Image, info.Network, "exited", nil)
	log.Info("sandbox stopped")
	fmt.Printf("sandbox stop: ok %s\n", name)
	return nil
}

// SandboxStart starts a previously stopped sandbox (volume retained).
func (a *App) SandboxStart(name string) error {
	const op = "cli.sandbox.start"
	log := a.op(op, slog.String("sandbox", name))
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("sandbox start: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	log.Info("starting sandbox")
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		log.Error("failed to inspect sandbox", slogx.Err(err))
		return err
	}
	if err := a.Sandboxes.Driver.Start(ctx, info.ID); err != nil {
		log.Error("failed to start sandbox", slogx.Err(err))
		return err
	}
	a.touchGateway(ctx, info.Name, string(info.ID), info.Image, info.Network, "running", nil)
	log.Info("sandbox started")
	fmt.Printf("sandbox start: ok %s\n", name)
	return nil
}

func (a *App) touchGateway(ctx context.Context, name, id, image, network, status string, labels map[string]string) {
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return
	}
	u := gwconfig.CurrentURL(cfg)
	if u == "" {
		return
	}
	_ = gatewayclient.New(u).UpsertSandbox(ctx, gatewayclient.Sandbox{
		Name: name, ID: id, Image: image, Network: network, Status: status, Labels: labels,
	})
}

// Copy transfers files: host→sandbox or sandbox→host.
// Specs look like "localpath" and "name:/path" (sandbox side has a colon).
func (a *App) Copy(src, dst string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("cp: docker not available")
	}
	ctx, cancel := a.withTimeout(TimeoutWork)
	defer cancel()
	sName, sPath, sOK := splitSandboxPath(src)
	dName, dPath, dOK := splitSandboxPath(dst)
	switch {
	case !sOK && dOK:
		if err := mounts.ValidateUploadDest(dPath); err != nil {
			return fmt.Errorf("cp: %w", err)
		}
		info, err := a.Sandboxes.Driver.Inspect(ctx, dName)
		if err != nil {
			return err
		}
		if err := a.Sandboxes.Driver.CopyTo(ctx, info.ID, src, dPath); err != nil {
			return err
		}
		fmt.Printf("cp: %s → %s:%s\n", src, dName, dPath)
	case sOK && !dOK:
		info, err := a.Sandboxes.Driver.Inspect(ctx, sName)
		if err != nil {
			return err
		}
		if err := a.Sandboxes.Driver.CopyFrom(ctx, info.ID, sPath, dst); err != nil {
			return err
		}
		fmt.Printf("cp: %s:%s → %s\n", sName, sPath, dst)
	default:
		return fmt.Errorf("usage: whaleshell cp <local> <sandbox>:/path  OR  whaleshell cp <sandbox>:/path <local>")
	}
	return nil
}

func splitSandboxPath(s string) (name, path string, ok bool) {
	// Avoid treating Windows drive letters; we only support name:/abs
	i := strings.Index(s, ":/")
	if i <= 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// ConnectSSH ensures guest sshd and prints/runs ssh to 127.0.0.1.
func (a *App) ConnectSSH(name string, open bool) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("connect: docker not available")
	}
	ctx, cancel := a.withTimeout(TimeoutWorkShort)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	port, err := a.Sandboxes.Driver.SSHPort(ctx, info.ID)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "whaleshell-ssh-client-*")
	if err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	_ = pub
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return err
	}
	privBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return err
	}
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(privBlock), 0o600); err != nil {
		return err
	}
	authLine := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	if err := a.Sandboxes.Driver.EnsureSSHDaemon(ctx, info.ID, strings.TrimSpace(authLine)); err != nil {
		return err
	}
	cmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i %s -p %d whaleshell@127.0.0.1", keyPath, port)
	fmt.Printf("ssh: %s\n", cmd)
	fmt.Printf("note: key kept at %s for this session\n", keyPath)
	if !open {
		return nil
	}
	c := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-i", keyPath,
		"-p", fmt.Sprintf("%d", port),
		"whaleshell@127.0.0.1",
	)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// SandboxSSHProxy dials the sandbox SSH port and bridges stdio (ProxyCommand).
func (a *App) SandboxSSHProxy(name string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("ssh-proxy: docker not available")
	}
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	port, err := a.Sandboxes.Driver.SSHPort(ctx, info.ID)
	if err != nil {
		return fmt.Errorf("ssh-proxy: enable SSH with --ssh on create: %w", err)
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), TimeoutAPI)
	if err != nil {
		return fmt.Errorf("ssh-proxy: dial: %w", err)
	}
	defer conn.Close()
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, os.Stdin)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(os.Stdout, conn)
		errCh <- err
	}()
	return <-errCh
}

// SandboxSSHConfig prints an OpenShell-style SSH Host block for a sandbox.
func (a *App) SandboxSSHConfig(name string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("ssh-config: docker not available")
	}
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	port, err := a.Sandboxes.Driver.SSHPort(ctx, info.ID)
	if err != nil {
		return fmt.Errorf("ssh-config: enable SSH with --ssh on create: %w", err)
	}
	host := "whaleshell-" + name
	fmt.Printf("Host %s\n", host)
	fmt.Printf("  HostName 127.0.0.1\n")
	fmt.Printf("  Port %d\n", port)
	fmt.Printf("  User whaleshell\n")
	fmt.Printf("  StrictHostKeyChecking no\n")
	fmt.Printf("  UserKnownHostsFile /dev/null\n")
	fmt.Printf("# append: whaleshell sandbox ssh-config %s >> ~/.ssh/config\n", name)
	return nil
}

// StartRelayAgent copies whaleshell-agent into the sandbox and starts it against the gateway.
func (a *App) StartRelayAgent(name, gatewayURL string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("relay: docker not available")
	}
	if gatewayURL == "" {
		cfg, _, err := gwconfig.Load()
		if err != nil {
			return err
		}
		gatewayURL = gwconfig.CurrentURL(cfg)
	}
	if gatewayURL == "" {
		return fmt.Errorf("relay: set --gateway or whaleshell gateway select")
	}
	// Inside Docker Desktop, agent should dial host.whaleshell.internal mapped port.
	guestGW := gatewayURL
	if strings.Contains(gatewayURL, "127.0.0.1") || strings.Contains(gatewayURL, "localhost") {
		u := strings.Replace(gatewayURL, "127.0.0.1", "host.whaleshell.internal", 1)
		u = strings.Replace(u, "localhost", "host.whaleshell.internal", 1)
		guestGW = u
	}
	ctx, cancel := a.withTimeout(TimeoutWorkShort)
	defer cancel()
	mod, err := findModuleDir("github.com/whaleshell/whaleshell-runtime")
	if err != nil {
		return err
	}
	bin, err := sidecar.EnsureLinuxAgent(ctx, mod)
	if err != nil {
		return err
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	_, _ = a.Sandboxes.Driver.Exec(ctx, info.ID, driver.ExecRequest{Argv: []string{"mkdir", "-p", "/whaleshell"}})
	if err := a.Sandboxes.Driver.CopyTo(ctx, info.ID, bin, "/whaleshell/whaleshell-agent"); err != nil {
		return err
	}
	_, err = a.Sandboxes.Driver.Exec(ctx, info.ID, driver.ExecRequest{
		Argv: []string{"sh", "-c", fmt.Sprintf(
			`chmod +x /whaleshell/whaleshell-agent; WHALESHELL_GATEWAY=%q WHALESHELL_SANDBOX=%q /whaleshell/whaleshell-agent --gateway %q --name %q >/whaleshell/agent.log 2>&1 &`,
			guestGW, name, guestGW, name)},
	})
	if err != nil {
		return err
	}
	fmt.Printf("relay agent: started in %s → %s\n", name, guestGW)
	return nil
}

// RelayExec sends argv to the sandbox agent via gateway long-poll relay.
func (a *App) RelayExec(name string, argv []string) error {
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return err
	}
	u := gwconfig.CurrentURL(cfg)
	if u == "" {
		return fmt.Errorf("relay exec: no current gateway")
	}
	c := gatewayclient.New(u)
	c.HTTP.Timeout = defaults.RelayClientTimeout
	ctx, cancel := a.withTimeout(defaults.RelayClientTimeout)
	defer cancel()
	out, err := c.Exec(ctx, name, argv)
	if err != nil {
		return err
	}
	fmt.Print(out.Output)
	if out.ExitCode != 0 {
		return fmt.Errorf("exit code %d", out.ExitCode)
	}
	return nil
}
