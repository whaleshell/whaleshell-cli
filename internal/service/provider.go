package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whaleshell/whaleshell-cli/internal/providerflags"
	"github.com/whaleshell/whaleshell-cli/internal/storage/gwconfig"
	"github.com/whaleshell/whaleshell-core/engine"
	"github.com/whaleshell/whaleshell-core/env"
	"github.com/whaleshell/whaleshell-core/policy"
	"github.com/whaleshell/whaleshell-providers/provider"
	"github.com/whaleshell/whaleshell-sdk/go/whaleshell"
	"gopkg.in/yaml.v3"
)

// ProviderProfileList lists builtin + custom profiles on the current gateway.
func (a *App) ProviderProfileList() error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	list, err := c.ListProfiles(a.apiCtx())
	if err != nil {
		return err
	}
	for _, p := range list {
		fmt.Printf("%s\t%s\n", p.ID, p.Source)
	}
	return nil
}

// ProviderProfileImport uploads a profile YAML to the gateway.
func (a *App) ProviderProfileImport(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	p, err := provider.ParseYAML(b)
	if err != nil {
		return err
	}
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	if err := c.PutProfile(a.apiCtx(), p.ID, b); err != nil {
		return err
	}
	fmt.Printf("imported profile %s\n", p.ID)
	return nil
}

// ProviderProfileShow prints a local builtin/custom file or validates path.
func (a *App) ProviderProfileShow(idOrPath string) error {
	return a.ProviderProfileShowFmt(idOrPath, "yaml")
}

// ProviderProfileShowFmt prints a profile as yaml or json wrapper.
func (a *App) ProviderProfileShowFmt(idOrPath, format string) error {
	var b []byte
	var err error
	if strings.Contains(idOrPath, "/") || strings.HasSuffix(idOrPath, ".yaml") || strings.HasSuffix(idOrPath, ".yml") {
		b, err = os.ReadFile(idOrPath)
	} else {
		dir := provider.FindBuiltinDir()
		if dir == "" {
			return fmt.Errorf("providers dir not found")
		}
		b, err = os.ReadFile(filepath.Join(dir, idOrPath+".yaml"))
	}
	if err != nil {
		return err
	}
	switch strings.ToLower(format) {
	case "json":
		var doc any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	default:
		fmt.Print(string(b))
		return nil
	}
}

// ProviderCreate registers an instance and stores credential values on the gateway
// (OpenShell-like). Values are never returned by list/get.
func (a *App) ProviderCreate(args providerflags.CreateArgs) error {
	credentials := args.Credentials
	if credentials == nil {
		credentials = map[string]string{}
	}
	prof, err := loadBuiltinProfile(args.Profile)
	if err != nil {
		return err
	}
	envVars := args.EnvVars
	if args.FromExisting || len(envVars) == 0 {
		discovered, err := prof.DiscoverEnvVars()
		if err != nil {
			return err
		}
		if args.FromExisting || len(envVars) == 0 {
			envVars = discovered
		}
	}
	for _, k := range envVars {
		if _, ok := credentials[k]; ok {
			continue
		}
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			credentials[k] = v
		}
	}
	if args.FromOIDCToken {
		cfg, _, err := gwconfig.Load()
		if err != nil {
			return err
		}
		tok := ""
		if cfg.Current != "" {
			tok = strings.TrimSpace(cfg.Gateways[cfg.Current].Token)
		}
		if tok == "" {
			return fmt.Errorf("provider create --from-oidc-token: no gateway token (run: whaleshell gateway login)")
		}
		key := "WHALESHELL_GATEWAY_TOKEN"
		if len(envVars) > 0 {
			key = envVars[0]
		}
		credentials[key] = tok
		if !containsString(envVars, key) {
			envVars = append(envVars, key)
		}
	}
	if args.FromGCloudADC {
		adc, err := readGCloudADC()
		if err != nil {
			return fmt.Errorf("provider create --from-gcloud-adc: %w", err)
		}
		key := "GOOGLE_APPLICATION_CREDENTIALS_JSON"
		credentials[key] = adc
		if !containsString(envVars, key) {
			envVars = append(envVars, key)
		}
	}
	if len(envVars) == 0 && len(credentials) > 0 {
		for k := range credentials {
			envVars = append(envVars, k)
		}
	}
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	rec := whaleshell.ProviderRecord{
		Name:                  args.Name,
		Type:                  args.Profile,
		EnvVars:               envVars,
		Credentials:           credentials,
		RuntimeCredentials:    args.RuntimeCredentials,
		CredentialExpiresAtMS: args.CredentialExpiresAt,
		Config:                args.Config,
	}
	if err := c.PutProvider(a.apiCtx(), rec); err != nil {
		return err
	}
	fmt.Printf("provider %s type=%s env=%v (values stored encrypted on gateway; never printed)\n", args.Name, args.Profile, envVars)
	return nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ProviderUpdate refreshes credential values for an existing instance.
func (a *App) ProviderUpdate(name string, fromExisting bool, credentials map[string]string) error {
	if credentials == nil {
		credentials = map[string]string{}
	}
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	list, err := c.ListProviders(a.apiCtx())
	if err != nil {
		return err
	}
	var rec whaleshell.ProviderRecord
	found := false
	for _, p := range list {
		if p.Name == name {
			rec = p
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("provider %q not found", name)
	}
	if fromExisting {
		for _, k := range rec.EnvVars {
			if _, ok := credentials[k]; ok {
				continue
			}
			if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
				credentials[k] = v
			}
		}
	}
	if len(credentials) == 0 {
		return fmt.Errorf("provider update: no credentials (use --from-existing or --credential)")
	}
	rec.Credentials = credentials
	if err := c.PutProvider(a.apiCtx(), rec); err != nil {
		return err
	}
	fmt.Printf("provider %s updated (values stored encrypted on gateway)\n", name)
	return nil
}

// prepareProviders resolves --provider flags: discover host env, ensure gateway
// instances, compose profile endpoints into the create-time policy (OpenShell-like).
func (a *App) prepareProviders(base policy.Document, basePath string, names []string, gwURL string) (attached []string, doc policy.Document, policyPath string, err error) {
	doc = base
	policyPath = basePath
	if len(names) == 0 {
		return nil, doc, policyPath, nil
	}
	var layers []provider.Layer
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		prof, envKeys, instName, err := a.resolveProviderForCreate(name, gwURL)
		if err != nil {
			return nil, base, basePath, err
		}
		layers = append(layers, provider.Layer{
			InstanceName: instName,
			Profile:      prof,
			EnvVars:      envKeys,
		})
		attached = append(attached, instName)
		fmt.Printf("provider: %s (type=%s env=%v)\n", instName, prof.ID, envKeys)
	}
	doc = provider.EffectivePolicy(base, layers, false)
	if err := doc.Validate(); err != nil {
		return nil, base, basePath, fmt.Errorf("provider compose: %w", err)
	}
	b, err := yaml.Marshal(doc)
	if err != nil {
		return nil, base, basePath, err
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	outDir := filepath.Join(dir, "whaleshell", "composed-policy")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, base, basePath, err
	}
	out := filepath.Join(outDir, fmt.Sprintf("%d.yaml", time.Now().UnixNano()))
	if err := os.WriteFile(out, b, 0o644); err != nil {
		return nil, base, basePath, err
	}
	return attached, doc, out, nil
}

func (a *App) resolveProviderForCreate(name, gwURL string) (provider.Profile, []string, string, error) {
	// Prefer existing gateway instance with this name (OpenShell: --provider <instance>).
	if gwURL != "" {
		c := a.clientFor(gwURL)
		list, err := c.ListProviders(a.apiCtx())
		if err != nil {
			return provider.Profile{}, nil, "", fmt.Errorf("provider %q: gateway %s: %w (is whaleshell-gateway running?)", name, gwURL, err)
		}
		for _, rec := range list {
			if rec.Name != name {
				continue
			}
			prof, err := loadBuiltinProfile(rec.Type)
			if err != nil {
				return provider.Profile{}, nil, "", fmt.Errorf("provider %q: profile type %q: %w", name, rec.Type, err)
			}
			keys := rec.EnvVars
			if len(keys) == 0 {
				keys, err = prof.DiscoverEnvVars()
				if err != nil {
					if allCredentialsOptional(prof) {
						keys = prof.EnvKeys()
					} else {
						return provider.Profile{}, nil, "", err
					}
				}
			}
			return prof, keys, rec.Name, nil
		}
	}
	// Treat name as profile id: discover env and auto-create instance (e.g. --provider github).
	prof, err := loadBuiltinProfile(name)
	if err != nil {
		hint := ""
		if gwURL != "" {
			hint = fmt.Sprintf(" (no gateway instance %q; create with: whaleshell provider create --name %s --type <profile> — or use --provider <profile-id>)", name, name)
		}
		return provider.Profile{}, nil, "", fmt.Errorf("provider %q: not a gateway instance or builtin profile%s: %w", name, hint, err)
	}
	keys, err := prof.DiscoverEnvVars()
	if err != nil {
		// Network-only / inject_env:false profiles (Cursor): still attach endpoints without host secrets.
		if allCredentialsOptional(prof) {
			keys = prof.EnvKeys()
		} else {
			return provider.Profile{}, nil, "", err
		}
	}
	if gwURL != "" {
		c := a.clientFor(gwURL)
		creds := map[string]string{}
		for _, k := range keys {
			if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" && !env.IsPlaceholder(v) {
				creds[k] = v
			}
		}
		if err := c.PutProvider(a.apiCtx(), whaleshell.ProviderRecord{
			Name: name, Type: prof.ID, EnvVars: keys, Credentials: creds,
		}); err != nil {
			return provider.Profile{}, nil, "", fmt.Errorf("provider %q: register on gateway: %w", name, err)
		}
	}
	return prof, keys, name, nil
}

func allCredentialsOptional(p provider.Profile) bool {
	if len(p.Credentials) == 0 {
		return true
	}
	for _, c := range p.Credentials {
		if c.Required {
			return false
		}
	}
	return true
}

func loadBuiltinProfile(idOrPath string) (provider.Profile, error) {
	if strings.Contains(idOrPath, "/") || strings.HasSuffix(idOrPath, ".yaml") || strings.HasSuffix(idOrPath, ".yml") {
		return provider.LoadFile(idOrPath)
	}
	dir := provider.FindBuiltinDir()
	if dir == "" {
		return provider.Profile{}, fmt.Errorf("providers dir not found (need whaleshell-cli/providers)")
	}
	path := filepath.Join(dir, idOrPath+".yaml")
	return provider.LoadFile(path)
}

// ProviderList lists gateway provider instances.
func (a *App) ProviderList() error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	list, err := c.ListProviders(a.apiCtx())
	if err != nil {
		return err
	}
	for _, p := range list {
		fmt.Printf("%s\ttype=%s\tenv=%v\n", p.Name, p.Type, p.EnvVars)
	}
	return nil
}

// ProviderAttach attaches a provider instance to a registered sandbox.
// When Docker is available, pushes composed effective policy so the proxy hot-reloads.
func (a *App) ProviderAttach(sandbox, providerName string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	if err := c.AttachProvider(a.apiCtx(), sandbox, providerName); err != nil {
		return err
	}
	fmt.Printf("attached %s → sandbox %s\n", providerName, sandbox)
	if err := a.applyEffectivePolicy(sandbox); err != nil {
		fmt.Printf("warn: could not apply effective policy (%v); run: whaleshell provider effective %s > /tmp/p.yaml && whaleshell policy set %s /tmp/p.yaml\n",
			err, sandbox, sandbox)
		return nil
	}
	return nil
}

// ProviderDetach detaches a provider from a sandbox and refreshes live policy when possible.
func (a *App) ProviderDetach(sandbox, providerName string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	if err := c.DetachProvider(a.apiCtx(), sandbox, providerName); err != nil {
		return err
	}
	fmt.Printf("detached %s from sandbox %s\n", providerName, sandbox)
	if err := a.applyEffectivePolicy(sandbox); err != nil {
		fmt.Printf("warn: could not apply effective policy (%v)\n", err)
	}
	return nil
}

// applyEffectivePolicy fetches gateway effective policy YAML and writes it to the sandbox policy bind.
func (a *App) applyEffectivePolicy(sandbox string) error {
	if a.Docker == nil {
		return fmt.Errorf("docker not available")
	}
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	b, err := c.EffectivePolicy(a.apiCtx(), sandbox)
	if err != nil {
		return err
	}
	doc, err := policy.Parse(b)
	if err != nil {
		return err
	}
	doc, err = a.mergeGatewayGlobal(doc)
	if err != nil {
		return err
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		return err
	}
	ctx, cancel := a.withTimeout(TimeoutAPILong)
	defer cancel()
	hostPath, err := a.Docker.PolicyHostPath(ctx, sandbox)
	if err != nil {
		return err
	}
	if err := writeFileInPlace(hostPath, b); err != nil {
		return err
	}
	fmt.Printf("policy applied: sandbox=%s allow_rules=%d (proxy reloads within ~1s)\n",
		sandbox, len(doc.AllowRules()))
	return nil
}

// ProviderEffective prints composed YAML for a sandbox.
func (a *App) ProviderEffective(sandbox string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	b, err := c.EffectivePolicy(a.apiCtx(), sandbox)
	if err != nil {
		return err
	}
	fmt.Print(string(b))
	return nil
}

func readGCloudADC() (string, error) {
	candidates := []string{}
	if p := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); p != "" {
		candidates = append(candidates, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"))
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err == nil && len(bytesTrim(b)) > 0 {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("ADC file not found (set GOOGLE_APPLICATION_CREDENTIALS or run gcloud auth application-default login)")
}
