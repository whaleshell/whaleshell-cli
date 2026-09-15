package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zorneth/osg-core/engine"
	"github.com/zorneth/osg-core/policy"
	"github.com/zorneth/osg-core/provider"
	"github.com/zorneth/osg-runtime/gatewayclient"
	"gopkg.in/yaml.v3"
)

func (a *App) gatewayClient() (*gatewayclient.Client, error) {
	u, err := currentGatewayURL()
	if err != nil {
		return nil, err
	}
	if u == "" {
		return nil, fmt.Errorf("no gateway selected (osg gateway add|select)")
	}
	return gatewayclient.New(u), nil
}

// ProviderProfileList lists builtin + custom profiles on the current gateway.
func (a *App) ProviderProfileList() error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	list, err := c.ListProfiles(context.Background())
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
	if err := c.PutProfile(context.Background(), p.ID, b); err != nil {
		return err
	}
	fmt.Printf("imported profile %s\n", p.ID)
	return nil
}

// ProviderProfileShow prints a local builtin/custom file or validates path.
func (a *App) ProviderProfileShow(idOrPath string) error {
	if strings.Contains(idOrPath, "/") || strings.HasSuffix(idOrPath, ".yaml") || strings.HasSuffix(idOrPath, ".yml") {
		b, err := os.ReadFile(idOrPath)
		if err != nil {
			return err
		}
		fmt.Print(string(b))
		return nil
	}
	dir := provider.FindBuiltinDir()
	if dir == "" {
		return fmt.Errorf("providers dir not found")
	}
	path := filepath.Join(dir, idOrPath+".yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Print(string(b))
	return nil
}

// ProviderCreate registers an instance and stores credential values on the gateway
// (OpenShell-like). Values are never returned by list/get.
// credentials maps env KEY → value (from --credential / --from-existing).
func (a *App) ProviderCreate(name, profileType string, envVars []string, fromExisting bool, credentials map[string]string) error {
	if credentials == nil {
		credentials = map[string]string{}
	}
	prof, err := loadBuiltinProfile(profileType)
	if err != nil {
		return err
	}
	if fromExisting || len(envVars) == 0 {
		discovered, err := prof.DiscoverEnvVars()
		if err != nil {
			return err
		}
		if fromExisting || len(envVars) == 0 {
			envVars = discovered
		}
	}
	// Copy values from host env for discovered / listed keys (OpenShell --from-existing).
	for _, k := range envVars {
		if _, ok := credentials[k]; ok {
			continue
		}
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			credentials[k] = v
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
	rec := gatewayclient.ProviderRecord{
		Name: name, Type: profileType, EnvVars: envVars, Credentials: credentials,
	}
	if err := c.PutProvider(context.Background(), rec); err != nil {
		return err
	}
	fmt.Printf("provider %s type=%s env=%v (values stored encrypted on gateway; never printed)\n", name, profileType, envVars)
	return nil
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
	list, err := c.ListProviders(context.Background())
	if err != nil {
		return err
	}
	var rec gatewayclient.ProviderRecord
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
	if err := c.PutProvider(context.Background(), rec); err != nil {
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
	doc = provider.Compose(base, layers, false)
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
	outDir := filepath.Join(dir, "osg", "composed-policy")
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
	// Prefer existing gateway instance with this name.
	if gwURL != "" {
		c := gatewayclient.New(gwURL)
		if list, err := c.ListProviders(context.Background()); err == nil {
			for _, rec := range list {
				if rec.Name != name {
					continue
				}
				prof, err := loadBuiltinProfile(rec.Type)
				if err != nil {
					return provider.Profile{}, nil, "", err
				}
				keys := rec.EnvVars
				if len(keys) == 0 {
					keys, err = prof.DiscoverEnvVars()
					if err != nil {
						return provider.Profile{}, nil, "", err
					}
				}
				return prof, keys, rec.Name, nil
			}
		}
	}
	// Treat name as profile id: discover env and auto-create instance.
	prof, err := loadBuiltinProfile(name)
	if err != nil {
		return provider.Profile{}, nil, "", err
	}
	keys, err := prof.DiscoverEnvVars()
	if err != nil {
		return provider.Profile{}, nil, "", err
	}
	if gwURL != "" {
		c := gatewayclient.New(gwURL)
		creds := map[string]string{}
		for _, k := range keys {
			if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
				creds[k] = v
			}
		}
		_ = c.PutProvider(context.Background(), gatewayclient.ProviderRecord{
			Name: name, Type: prof.ID, EnvVars: keys, Credentials: creds,
		})
	}
	return prof, keys, name, nil
}

func loadBuiltinProfile(idOrPath string) (provider.Profile, error) {
	if strings.Contains(idOrPath, "/") || strings.HasSuffix(idOrPath, ".yaml") || strings.HasSuffix(idOrPath, ".yml") {
		return provider.LoadFile(idOrPath)
	}
	dir := provider.FindBuiltinDir()
	if dir == "" {
		return provider.Profile{}, fmt.Errorf("providers dir not found (need osg-cli/providers)")
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
	list, err := c.ListProviders(context.Background())
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
	if err := c.AttachProvider(context.Background(), sandbox, providerName); err != nil {
		return err
	}
	fmt.Printf("attached %s → sandbox %s\n", providerName, sandbox)
	if err := a.applyEffectivePolicy(sandbox); err != nil {
		fmt.Printf("warn: could not apply effective policy (%v); run: osg provider effective %s > /tmp/p.yaml && osg policy set %s /tmp/p.yaml\n",
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
	if err := c.DetachProvider(context.Background(), sandbox, providerName); err != nil {
		return err
	}
	fmt.Printf("detached %s from sandbox %s\n", providerName, sandbox)
	if err := a.applyEffectivePolicy(sandbox); err != nil {
		fmt.Printf("warn: could not apply effective policy (%v)\n", err)
	}
	return nil
}

// applyEffectivePolicy fetches gateway composed YAML and writes it to the sandbox policy bind.
func (a *App) applyEffectivePolicy(sandbox string) error {
	if a.Docker == nil {
		return fmt.Errorf("docker not available")
	}
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	b, err := c.EffectivePolicy(context.Background(), sandbox)
	if err != nil {
		return err
	}
	doc, err := policy.Parse(b)
	if err != nil {
		return err
	}
	doc, err = mergeGatewayGlobal(doc)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	b, err := c.EffectivePolicy(context.Background(), sandbox)
	if err != nil {
		return err
	}
	fmt.Print(string(b))
	return nil
}
