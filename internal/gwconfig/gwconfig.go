// Package gwconfig loads multi-gateway CLI config (~/.config/osg/config.yaml).
package gwconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zorneth/osg-core/defaults"
	"gopkg.in/yaml.v3"
)

// File is the on-disk CLI config.
type File struct {
	Current  string             `yaml:"current,omitempty"`
	Gateways map[string]Gateway `yaml:"gateways,omitempty"`
	// Images maps BYOC / community short names to container images (--from).
	Images map[string]string `yaml:"images,omitempty"`
}

// Gateway is one named control-plane endpoint.
type Gateway struct {
	URL     string `yaml:"url"`
	DataDir string `yaml:"data_dir,omitempty"`
	Token   string `yaml:"token,omitempty"` // access token (local-dev or OIDC)

	// OIDC (OpenShell-compatible gateway add flags).
	OIDCIssuer       string `yaml:"oidc_issuer,omitempty"`
	OIDCClientID     string `yaml:"oidc_client_id,omitempty"`
	OIDCAudience     string `yaml:"oidc_audience,omitempty"`
	OIDCScopes       string `yaml:"oidc_scopes,omitempty"`
	OIDCAllowHTTP    bool   `yaml:"oidc_allow_insecure_http,omitempty"`
	RefreshToken     string `yaml:"refresh_token,omitempty"`
	TokenExpiresAtMS int64  `yaml:"token_expires_at_ms,omitempty"`
}

// Path returns the default config path.
func Path() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "osg", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "osg", "config.yaml"), nil
}

// Load reads config or returns empty defaults.
func Load() (File, string, error) {
	p, err := Path()
	if err != nil {
		return File{}, "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return File{Gateways: map[string]Gateway{}}, p, nil
		}
		return File{}, p, err
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return File{}, p, fmt.Errorf("config: %w", err)
	}
	if f.Gateways == nil {
		f.Gateways = map[string]Gateway{}
	}
	if f.Images == nil {
		f.Images = map[string]string{}
	}
	return f, p, nil
}

// Save writes config atomically.
func Save(f File) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// CurrentURL returns the selected gateway URL if any.
func CurrentURL(f File) string {
	if f.Current == "" {
		return ""
	}
	g, ok := f.Gateways[f.Current]
	if !ok {
		return ""
	}
	return g.URL
}

// ResolveImage expands --from aliases (config images map + builtins).
func ResolveImage(from, explicit string) (string, error) {
	if explicit != "" && from != "" {
		return "", fmt.Errorf("use either --image or --from, not both")
	}
	if explicit != "" {
		return explicit, nil
	}
	if from == "" {
		return "", nil
	}
	cfg, _, err := Load()
	if err != nil {
		return "", err
	}
	if img, ok := cfg.Images[from]; ok && img != "" {
		return img, nil
	}
	builtins := map[string]string{
		"ubuntu": "ubuntu:24.04",
		"debian": defaults.ImageDebian,
		// osg catalog shorts (local). GHCR paths when OSG_USE_GHCR=1.
		"base":    defaults.ImageLocal,
		"osg/cli": defaults.ImageLocal,
		"osg/gui": defaults.ImageGUI,
		"osg/gpu": defaults.ImageGPU,
		"cursor":  defaults.ImageCursor,
		"claude":  defaults.ImageClaude,
		"codex":   defaults.ImageCodex,
		// Opt-in NVIDIA OpenShell community interop.
		"community/base":   "ghcr.io/nvidia/openshell-community/sandboxes/base:latest",
		"community/ollama": "ghcr.io/nvidia/openshell-community/sandboxes/ollama:latest",
		"ollama":           "ghcr.io/nvidia/openshell-community/sandboxes/ollama:latest",
	}
	// After images:pull / CI publish, set images.* in config or OSG_USE_GHCR=1 for GHCR builtins.
	if os.Getenv("OSG_USE_GHCR") == "1" {
		builtins["base"] = defaults.ImageBaseRef
		builtins["osg/cli"] = defaults.ImageBaseRef
		builtins["osg/gui"] = defaults.ImageGUIRef
		builtins["osg/gpu"] = defaults.ImageGPURef
		builtins["cursor"] = defaults.ImageCursorRef
		builtins["claude"] = defaults.ImageClaudeRef
		builtins["codex"] = defaults.ImageCodexRef
	}
	if img, ok := builtins[from]; ok {
		return img, nil
	}
	return "", fmt.Errorf("unknown --from %q (add images.%s to ~/.config/osg/config.yaml)", from, from)
}
