package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zorneth/osg-cli/internal/gwconfig"
	"github.com/zorneth/osg-core/defaults"
	"github.com/zorneth/osg-runtime/driver"
	"github.com/zorneth/osg-runtime/gatewayclient"
	"github.com/zorneth/osg-runtime/sidecar"
	"golang.org/x/crypto/ssh"
)

// SandboxStop stops a sandbox without deleting network/volume.
func (a *App) SandboxStop(name string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox stop: docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	if err := a.Sandboxes.Driver.Stop(ctx, info.ID); err != nil {
		return err
	}
	a.touchGateway(ctx, info.Name, string(info.ID), info.Image, info.Network, "exited", nil)
	fmt.Printf("sandbox stop: ok %s\n", name)
	return nil
}

// SandboxStart starts a previously stopped sandbox (volume retained).
func (a *App) SandboxStart(name string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox start: docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	if err := a.Sandboxes.Driver.Start(ctx, info.ID); err != nil {
		return err
	}
	a.touchGateway(ctx, info.Name, string(info.ID), info.Image, info.Network, "running", nil)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sName, sPath, sOK := splitSandboxPath(src)
	dName, dPath, dOK := splitSandboxPath(dst)
	switch {
	case !sOK && dOK:
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
		return fmt.Errorf("usage: osg cp <local> <sandbox>:/path  OR  osg cp <sandbox>:/path <local>")
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	port, err := a.Sandboxes.Driver.SSHPort(ctx, info.ID)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "osg-ssh-client-*")
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
	cmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i %s -p %d osg@127.0.0.1", keyPath, port)
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
		"osg@127.0.0.1",
	)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// StartRelayAgent copies osg-agent into the sandbox and starts it against the gateway.
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
		return fmt.Errorf("relay: set --gateway or osg gateway select")
	}
	// Inside Docker Desktop, agent should dial host.osg.internal mapped port.
	guestGW := gatewayURL
	if strings.Contains(gatewayURL, "127.0.0.1") || strings.Contains(gatewayURL, "localhost") {
		u := strings.Replace(gatewayURL, "127.0.0.1", "host.osg.internal", 1)
		u = strings.Replace(u, "localhost", "host.osg.internal", 1)
		guestGW = u
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	mod, err := findModuleDir("github.com/zorneth/osg-runtime")
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
	_, _ = a.Sandboxes.Driver.Exec(ctx, info.ID, driver.ExecRequest{Argv: []string{"mkdir", "-p", "/osg"}})
	if err := a.Sandboxes.Driver.CopyTo(ctx, info.ID, bin, "/osg/osg-agent"); err != nil {
		return err
	}
	_, err = a.Sandboxes.Driver.Exec(ctx, info.ID, driver.ExecRequest{
		Argv: []string{"sh", "-c", fmt.Sprintf(
			`chmod +x /osg/osg-agent; OSG_GATEWAY=%q OSG_SANDBOX=%q /osg/osg-agent --gateway %q --name %q >/osg/agent.log 2>&1 &`,
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
	ctx, cancel := context.WithTimeout(context.Background(), defaults.RelayClientTimeout)
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
