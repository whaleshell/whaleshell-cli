package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/whaleshell/whaleshell-driver/driver"
)

// Linux helper binaries mounted into sandboxes and proxy sidecars. Release
// archives ship them under <prefix>/libexec/whaleshell/linux-<arch>/ next to
// <prefix>/bin/whaleshell; source checkouts cross-compile them on demand.
const (
	helperCLI  = "whaleshell"
	helperInit = "whaleshell-init"
	helperSSHD = "whaleshell-sshd"

	// EnvHelpersDir pins the directory holding linux helpers (no fallbacks).
	EnvHelpersDir = "WHALESHELL_HELPERS_DIR"
)

// helperDirs lists where bundled linux helpers may live for this install.
func helperDirs() []string {
	if d := os.Getenv(EnvHelpersDir); d != "" {
		return []string{d}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	dir := filepath.Dir(exe)
	sub := filepath.Join("libexec", "whaleshell", "linux-"+runtime.GOARCH)
	return []string{
		filepath.Join(dir, "..", sub),
		filepath.Join(dir, sub),
	}
}

func bundledHelper(name string) (string, bool) {
	for _, d := range helperDirs() {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return filepath.Clean(p), true
		}
	}
	return "", false
}

// resolveLinuxHelper finds a linux/<arch> helper: bundled with the install,
// else cross-compiled from a source checkout, else (CLI on linux) this binary.
func resolveLinuxHelper(ctx context.Context, name, module string, build func(context.Context, string) (string, error)) (string, error) {
	if p, ok := bundledHelper(name); ok {
		return p, nil
	}
	if d := os.Getenv(EnvHelpersDir); d != "" {
		return "", fmt.Errorf("%s not found in %s=%s", name, EnvHelpersDir, d)
	}
	var srcErr error
	if mod, err := findModuleDir(module); err != nil {
		srcErr = err
	} else if _, err := exec.LookPath("go"); err != nil {
		srcErr = fmt.Errorf("source checkout at %s but no Go toolchain", mod)
	} else {
		return build(ctx, mod)
	}
	if name == helperCLI && runtime.GOOS == "linux" {
		if exe, err := os.Executable(); err == nil {
			return exe, nil
		}
	}
	return "", fmt.Errorf("linux %s helper not installed (%v); reinstall with install.sh (ships libexec/whaleshell/linux-%s) or set %s",
		name, srcErr, runtime.GOARCH, EnvHelpersDir)
}

func ensureProxyBin(ctx context.Context) (string, error) {
	return resolveLinuxHelper(ctx, helperCLI, "github.com/whaleshell/whaleshell-cli", driver.EnsureLinuxCLI)
}

func ensureInitBin(ctx context.Context) (string, error) {
	return resolveLinuxHelper(ctx, helperInit, "github.com/whaleshell/whaleshell-runtime", driver.EnsureLinuxInit)
}

func ensureSSHDBin(ctx context.Context) (string, error) {
	return resolveLinuxHelper(ctx, helperSSHD, "github.com/whaleshell/whaleshell-runtime", driver.EnsureLinuxSSHD)
}

// helperStatus reports where linux helpers come from, without building.
func helperStatus() string {
	var missing []string
	dir := ""
	for _, n := range []string{helperCLI, helperInit, helperSSHD} {
		if p, ok := bundledHelper(n); ok {
			dir = filepath.Dir(p)
		} else {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return "bundled (" + dir + ")"
	}
	if _, err := findModuleDir("github.com/whaleshell/whaleshell-runtime"); err == nil {
		if _, err := exec.LookPath("go"); err == nil {
			return "source checkout (cross-compiled on demand)"
		}
	}
	return fmt.Sprintf("missing %v (reinstall with install.sh)", missing)
}
