package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zorneth/osg-core/defaults"
	"github.com/zorneth/osg-sdk/gatewayclient"
)

const localGatewayName = "local"
const localGatewayURL = "http://" + defaults.GatewayListen

// GatewayEnsure makes sure a reachable gateway is selected (OpenShell install UX).
// If the current gateway is healthy, it is a no-op. Otherwise it starts a local
// osg-gateway on 127.0.0.1:7443 (sibling binary or PATH), registers it as "local",
// and selects it.
func (a *App) GatewayEnsure() error {
	if u, err := a.currentGatewayURL(); err == nil && u != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := gatewayclient.New(u).Healthz(ctx); err == nil {
			return nil
		}
	}
	// Prefer already-listening local port (user started gateway manually).
	if portOpen("127.0.0.1", defaults.GatewayPort) {
		_ = a.GatewayAdd(localGatewayName, localGatewayURL)
		_ = a.GatewaySelect(localGatewayName)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := gatewayclient.New(localGatewayURL).Healthz(ctx); err == nil {
			fmt.Printf("gateway ensure: using existing %s\n", localGatewayURL)
			return nil
		}
	}
	bin, err := findGatewayBinary()
	if err != nil {
		return fmt.Errorf("gateway ensure: %w\nStart manually: osg-gateway --listen %s\nThen: osg gateway add local --url %s && osg gateway select local",
			err, defaults.GatewayListen, localGatewayURL)
	}
	logPath, err := gatewayLogPath()
	if err != nil {
		return err
	}
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("gateway ensure: log: %w", err)
	}
	cmd := exec.Command(bin, "--listen", defaults.GatewayListen)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = gatewaySysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return fmt.Errorf("gateway ensure: start %s: %w", bin, err)
	}
	// Detach: do not wait; log file stays open in child.
	go func() {
		_ = cmd.Wait()
		_ = logF.Close()
	}()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, err := gatewayclient.New(localGatewayURL).Healthz(ctx)
		cancel()
		if err == nil {
			_ = a.GatewayAdd(localGatewayName, localGatewayURL)
			_ = a.GatewaySelect(localGatewayName)
			fmt.Printf("gateway ensure: started %s (pid %d, log %s)\n", localGatewayURL, cmd.Process.Pid, logPath)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("gateway ensure: started %s but healthz not ready (see %s)", bin, logPath)
}

func portOpen(host string, port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func findGatewayBinary() (string, error) {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "osg-gateway"),
			filepath.Join(dir, "osg-gateway-darwin-arm64"),
			filepath.Join(dir, "osg-gateway-darwin-amd64"),
			filepath.Join(dir, "osg-gateway-linux-arm64"),
			filepath.Join(dir, "osg-gateway-linux-amd64"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "osg-gateway"),
			filepath.Join(wd, "osg-cli", "osg-gateway"),
			filepath.Join(wd, "osg-gateway", "osg-gateway"),
		)
	}
	if p, err := exec.LookPath("osg-gateway"); err == nil {
		candidates = append(candidates, p)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0 {
			return c, nil
		}
	}
	return "", fmt.Errorf("osg-gateway binary not found next to osg or on PATH")
}

func gatewayLogPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "osg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "gateway.log"), nil
}

// InferProvidersFromArgv maps trailing create commands to provider profile ids
// (OpenShell detect_provider_from_command).
func InferProvidersFromArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "agent", "cursor-agent", "cursor":
		return []string{"cursor"}
	case "claude", "claude-code":
		return []string{"claude-code"}
	case "codex":
		return []string{"codex"}
	case "gh", "git":
		return []string{"github"}
	default:
		return nil
	}
}
