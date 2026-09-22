package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/whaleshell/whaleshell-cli/internal/global"
	"github.com/whaleshell/whaleshell-cli/internal/storage/gwconfig"
	"github.com/whaleshell/whaleshell-sdk/gatewayclient"
)

// ApplyGlobal stores OpenShell-style global flags for this CLI session.
func (a *App) ApplyGlobal(g global.Context) {
	if g.Output != "" {
		a.OutputFormat = g.Output
	}
	if g.Workspace != "" {
		a.GlobalWorkspace = g.Workspace
	}
	a.GatewayURLOverride = strings.TrimSpace(g.GatewayURL)
	a.GatewayNameOverride = strings.TrimSpace(g.GatewayName)
}

func looksLikeGatewayURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, "://") || strings.HasPrefix(s, "127.") || strings.HasPrefix(s, "localhost")
}

// currentGatewayURL resolves the active gateway endpoint.
// Order: App URL override, OPENSHELL_GATEWAY/WHALESHELL_GATEWAY_URL, -g name, config current.
func (a *App) currentGatewayURL() (string, error) {
	if a != nil && a.GatewayURLOverride != "" {
		return strings.TrimRight(a.GatewayURLOverride, "/"), nil
	}
	if v := firstNonEmptyEnv("OPENSHELL_GATEWAY", "WHALESHELL_GATEWAY_URL"); v != "" {
		if looksLikeGatewayURL(v) {
			return strings.TrimRight(v, "/"), nil
		}
		cfg, _, err := gwconfig.Load()
		if err != nil {
			return "", err
		}
		if g, ok := cfg.Gateways[v]; ok && g.URL != "" {
			return strings.TrimRight(g.URL, "/"), nil
		}
		return "", fmt.Errorf("gateway %q from env not in config (whaleshell gateway add)", v)
	}
	if a != nil && a.GatewayNameOverride != "" {
		cfg, _, err := gwconfig.Load()
		if err != nil {
			return "", err
		}
		g, ok := cfg.Gateways[a.GatewayNameOverride]
		if !ok || g.URL == "" {
			return "", fmt.Errorf("gateway %q not in config", a.GatewayNameOverride)
		}
		return strings.TrimRight(g.URL, "/"), nil
	}
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return "", err
	}
	u := gwconfig.CurrentURL(cfg)
	if u == "" {
		return "", fmt.Errorf("no current gateway; run: whaleshell gateway add|select")
	}
	return strings.TrimRight(u, "/"), nil
}

func (a *App) gatewayTokenForURL(url string) string {
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return ""
	}
	url = strings.TrimRight(url, "/")
	for _, g := range cfg.Gateways {
		if strings.TrimRight(g.URL, "/") == url {
			return strings.TrimSpace(g.Token)
		}
	}
	return ""
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func (a *App) gatewayClient() (*gatewayclient.Client, error) {
	if err := a.GatewayEnsure(); err != nil {
		return nil, err
	}
	u, err := a.currentGatewayURL()
	if err != nil {
		return nil, err
	}
	if u == "" {
		return nil, fmt.Errorf("no gateway selected (whaleshell gateway ensure|add|select)")
	}
	tok := a.gatewayTokenForURL(u)
	return gatewayclient.NewWithToken(u, tok), nil
}
