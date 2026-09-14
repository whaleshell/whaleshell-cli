package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zorneth/osg-core/provider"
	"github.com/zorneth/osg-runtime/gatewayclient"
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

// ProviderCreate registers an instance (env key refs only).
func (a *App) ProviderCreate(name, profileType string, envVars []string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	rec := gatewayclient.ProviderRecord{Name: name, Type: profileType, EnvVars: envVars}
	if err := c.PutProvider(context.Background(), rec); err != nil {
		return err
	}
	fmt.Printf("provider %s type=%s (set host env for keys; values not stored on gateway)\n", name, profileType)
	return nil
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
func (a *App) ProviderAttach(sandbox, providerName string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	if err := c.AttachProvider(context.Background(), sandbox, providerName); err != nil {
		return err
	}
	fmt.Printf("attached %s → sandbox %s\n", providerName, sandbox)
	fmt.Println("next: osg provider effective", sandbox)
	return nil
}

// ProviderDetach detaches a provider from a sandbox.
func (a *App) ProviderDetach(sandbox, providerName string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	return c.DetachProvider(context.Background(), sandbox, providerName)
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
