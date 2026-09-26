package service

import (
	"context"
	"fmt"
	"os"

	"github.com/whaleshell/whaleshell-cli/internal/storage/gwconfig"
	"github.com/whaleshell/whaleshell-core/policy"
	"github.com/whaleshell/whaleshell-driver/driver"
	"github.com/whaleshell/whaleshell-sdk/go/whaleshell"
)

// attachSupervisor wires the proxy sidecar as the sandbox supervisor
// (OpenShell model): the sandbox is registered with the gateway before the
// sidecar starts, a sandbox-scoped token is minted for the sidecar only, and
// the in-sandbox sshd is enabled so IDE / connect traffic flows through the
// gateway relay. Requires spec.ProxyBin.
func (a *App) attachSupervisor(ctx context.Context, spec *driver.Spec, doc policy.Document, name, gwURL string) error {
	spec.ProxyEnv = proxySecretsForPolicy(doc)
	if gwURL == "" {
		if cfg, _, err := gwconfig.Load(); err == nil {
			gwURL = gwconfig.CurrentURL(cfg)
		}
	}
	if gwURL == "" {
		fmt.Fprintf(os.Stderr, "sandbox create: warn: no gateway selected; SSH / IDE access disabled\n")
		return nil
	}
	c := a.clientFor(gwURL)
	if err := c.UpsertSandbox(ctx, whaleshell.Sandbox{Name: name, Image: spec.Image, Status: "creating", Labels: spec.Labels}); err != nil {
		return fmt.Errorf("register sandbox with gateway %s: %w (run: whaleshell gateway login)", gwURL, err)
	}
	tok, err := c.IssueSandboxToken(ctx, name)
	if err != nil {
		return fmt.Errorf("sandbox supervisor token: %w", err)
	}
	spec.ProxyEnv = append(spec.ProxyEnv, a.proxyGatewayEnv(name, gwURL, tok)...)
	sshBin, err := ensureSSHDBin(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox create: warn: whaleshell-sshd unavailable (%v); SSH / IDE access disabled\n", err)
		return nil
	}
	spec.SSHBin = sshBin
	spec.EnableSSH = true
	return nil
}
