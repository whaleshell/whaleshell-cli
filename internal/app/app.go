// Package app wires concrete runtime / display backends for the CLI.
package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zorneth/osg-cli/internal/autoprovider"
	"github.com/zorneth/osg-cli/internal/gwconfig"
	"github.com/zorneth/osg-cli/internal/osargs"
	"github.com/zorneth/osg-cli/internal/policywait"
	"github.com/zorneth/osg-cli/internal/templates"
	"github.com/zorneth/osg-cli/internal/ui"
	"github.com/zorneth/osg-core/defaults"
	"github.com/zorneth/osg-core/engine"
	"github.com/zorneth/osg-core/env"
	"github.com/zorneth/osg-core/policy"
	"github.com/zorneth/osg-display"
	"github.com/zorneth/osg-driver/driver"
	dockerdriver "github.com/zorneth/osg-driver/driver/docker"
	"github.com/zorneth/osg-driver/driver/kubernetes"
	"github.com/zorneth/osg-driver/driver/podman"
	"github.com/zorneth/osg-driver/driver/vm"
	"github.com/zorneth/osg-driver/mounts"
	"github.com/zorneth/osg-driver/sidecar"
	"github.com/zorneth/osg-proxy/proxy"
	"github.com/zorneth/osg-runtime/inference"
	"github.com/zorneth/osg-runtime/sandbox"
	"github.com/zorneth/osg-runtime/secrets"
	"github.com/zorneth/osg-sdk/gatewayclient"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// App holds constructed dependencies for CLI commands.
type App struct {
	Sandboxes  *sandbox.Manager
	Display    display.Stack
	Docker     *dockerdriver.Driver // Engine API client (Docker or Podman)
	DriverName string               // "docker" | "podman" | "vm" | "kubernetes"

	// OpenShell global session (ApplyGlobal).
	OutputFormat        string
	GlobalWorkspace     string
	GatewayURLOverride  string
	GatewayNameOverride string
}

// New builds the default host-side graph.
func New() *App {
	a := &App{Display: display.None{}, DriverName: selectedDriver()}
	switch a.DriverName {
	case "vm":
		a.Sandboxes = &sandbox.Manager{Driver: vm.New()}
		return a
	case "kubernetes":
		a.Sandboxes = &sandbox.Manager{Driver: kubernetes.New()}
		return a
	}
	var d *dockerdriver.Driver
	var err error
	switch a.DriverName {
	case "podman":
		d, err = podman.New()
	default:
		d, err = dockerdriver.New()
		a.DriverName = "docker"
	}
	if err != nil {
		a.Sandboxes = &sandbox.Manager{}
		return a
	}
	a.Docker = d
	a.Sandboxes = &sandbox.Manager{
		Driver: d,
		Proxy:  proxy.NewCONNECT(&engine.Allowlist{}),
	}
	return a
}

func selectedDriver() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OSG_DRIVER"))) {
	case "podman":
		return "podman"
	case "vm", "microvm":
		return "vm"
	case "kubernetes", "k8s":
		return "kubernetes"
	default:
		return "docker"
	}
}

func envTruthy(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// Banner is the short CLI intro.
func (a *App) Banner() string {
	return "osg — agent sandbox CLI"
}

// Version reports the CLI stub version.
func (a *App) Version() string { return "osg 0.1.0-alpha.1" }

// Health probes Docker Engine / Podman API.
func (a *App) Health() error {
	if a.Docker == nil {
		hint := "check DOCKER_HOST / Docker Desktop"
		switch a.DriverName {
		case "podman":
			hint = "check OSG_PODMAN_SOCKET / podman.socket (systemctl --user start podman.socket)"
		case "vm":
			return fmt.Errorf("health: vm driver is a spike stub (see docs/exp/MICROVM.md)")
		case "kubernetes":
			return fmt.Errorf("health: kubernetes driver is a spike stub (see docs/exp/KUBERNETES.md)")
		}
		return fmt.Errorf("health: %s client unavailable (%s)", a.DriverName, hint)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := a.Docker.Health(ctx)
	if !p.OK {
		return fmt.Errorf("health: %s unreachable: %s", a.DriverName, p.Error)
	}
	fmt.Printf("health: ok\n")
	fmt.Printf("  driver:           %s\n", a.DriverName)
	fmt.Printf("  docker.server:    %s\n", p.ServerVersion)
	fmt.Printf("  docker.api:       %s\n", p.APIVersion)
	fmt.Printf("  docker.os:        %s\n", p.OperatingSystem)
	fmt.Printf("  docker.arch:      %s\n", p.Architecture)
	fmt.Printf("  docker.context:   %s\n", p.Context)
	fmt.Printf("  isolation:        %s\n", p.Isolation)
	fmt.Printf("  host.goos:        %s\n", p.HostGOOS)
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer probeCancel()
	probe := a.probeLandlock(probeCtx)
	fmt.Printf("  landlock:         %s\n", probe)
	fmt.Printf("  seccomp:          %s\n", dockerdriver.SeccompNote())
	for _, img := range []string{defaults.ImageLocal, defaults.ImageCursor} {
		ok := a.Docker.ImagePresent(ctx, img)
		state := "missing (task runtime:image:cli / task docker:agent:cursor)"
		if ok {
			state = "present"
		}
		fmt.Printf("  image %-18s %s\n", img+":", state)
	}
	if u, err := a.currentGatewayURL(); err == nil && u != "" {
		gctx, gcancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer gcancel()
		cli := gatewayclient.New(u)
		if _, err := cli.Healthz(gctx); err != nil {
			fmt.Printf("  gateway:          unreachable (%s)\n", u)
		} else {
			fmt.Printf("  gateway:          ok (%s)\n", u)
			if info, err := cli.Info(gctx); err == nil {
				printSecretsKEK(info)
			}
		}
	} else {
		fmt.Printf("  gateway:          not selected (osg gateway ensure)\n")
	}
	if envTruthy("OSG_LANDLOCK_REQUIRED") && !landlockABIAtLeast(probe, 1) {
		return fmt.Errorf("health: landlock gate failed (need ABI≥1; got %s). Unset OSG_LANDLOCK_REQUIRED on Docker Desktop / ABI 0 hosts", probe)
	}
	return nil
}

func printSecretsKEK(info map[string]any) {
	raw, ok := info["secrets_kek"].(map[string]any)
	if !ok {
		return
	}
	src, _ := raw["source"].(string)
	pinned, _ := raw["pinned"].(bool)
	fmt.Printf("  secrets_kek:      source=%s pinned=%v\n", src, pinned)
	if w, _ := raw["warning"].(string); strings.TrimSpace(w) != "" {
		fmt.Printf("  secrets_kek.warn: %s\n", w)
	}
}

// Doctor is the production Docker-path readiness check (OpenShell doctor alias).
func (a *App) Doctor() error {
	if err := a.Health(); err != nil {
		return err
	}
	u, err := a.currentGatewayURL()
	if err != nil || u == "" {
		return fmt.Errorf("doctor: no gateway selected (osg gateway ensure|add|select)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli := gatewayclient.New(u)
	info, err := cli.Info(ctx)
	if err != nil {
		return fmt.Errorf("doctor: gateway info: %w", err)
	}
	if raw, ok := info["secrets_kek"].(map[string]any); ok {
		if pinned, _ := raw["pinned"].(bool); !pinned {
			fmt.Fprintf(os.Stderr, "doctor: warn: pin %s in compose/env so secrets survive volume loss\n", secrets.EnvKEK)
		}
	}
	providers, err := cli.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("doctor: list providers: %w", err)
	}
	fmt.Printf("  providers:        %d instance(s)\n", len(providers))
	for _, p := range providers {
		fmt.Printf("    - %s type=%s env=%v\n", p.Name, p.Type, p.EnvVars)
	}
	fmt.Printf("doctor: ok (docker path)\n")
	return nil
}

func landlockABIAtLeast(probe string, min int) bool {
	// probe examples: "abi=2 (probe ok)", "abi=0 (...)"
	const p = "abi="
	i := strings.Index(strings.ToLower(probe), p)
	if i < 0 {
		return false
	}
	rest := probe[i+len(p):]
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n >= min
}

func (a *App) probeLandlock(ctx context.Context) string {
	initBin, err := ensureInitBin(ctx)
	if err != nil {
		return "probe skipped (" + err.Error() + ")"
	}
	if a.Docker == nil {
		return "docker unavailable"
	}
	out, err := a.Docker.RunProbe(ctx, initBin)
	if err != nil {
		return "probe error: " + err.Error()
	}
	return out
}

// PolicyCheck loads and validates a policy YAML file.
func (a *App) PolicyCheck(path string) error {
	doc, err := policy.Load(path)
	if err != nil {
		return fmt.Errorf("policy check: %w", err)
	}
	doc, err = a.mergeGatewayGlobal(doc)
	if err != nil {
		return fmt.Errorf("policy check: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("policy check: %w", err)
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		return fmt.Errorf("policy check: engine: %w", err)
	}
	fmt.Printf("policy check: ok version=%d harden=%s allow_rules=%d include_workdir=%v",
		doc.Version, doc.HardenMode(), len(doc.AllowRules()),
		doc.IncludeWorkdir())
	if doc.Inference != nil && len(doc.Inference.Providers) > 0 {
		fmt.Printf(" inference=%v", doc.Inference.Providers)
	}
	if len(doc.Binaries) > 0 {
		fmt.Printf(" binaries=%d", len(doc.Binaries))
	}
	if doc.RegoPath != "" {
		fmt.Printf(" rego_path=%s", doc.RegoPath)
	}
	fmt.Println()
	return nil
}

// PolicyGlobalGet prints gateway global policy YAML.
func (a *App) PolicyGlobalGet() error {
	u, err := a.currentGatewayURL()
	if err != nil {
		return err
	}
	b, err := gatewayclient.NewWithToken(u, a.gatewayTokenForURL(u)).GetGlobalPolicy(context.Background())
	if err != nil {
		return err
	}
	os.Stdout.Write(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

// PolicyGlobalSet uploads YAML file as gateway global policy.
func (a *App) PolicyGlobalSet(path string) error {
	u, err := a.currentGatewayURL()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := policy.Parse(b)
	if err != nil {
		return err
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	if err := gatewayclient.NewWithToken(u, a.gatewayTokenForURL(u)).PutGlobalPolicy(context.Background(), b); err != nil {
		return err
	}
	fmt.Printf("policy global set: ok url=%s bytes=%d\n", u, len(b))
	return nil
}

// PolicyGlobalClear removes the gateway-global policy lock.
func (a *App) PolicyGlobalClear() error {
	u, err := a.currentGatewayURL()
	if err != nil {
		return err
	}
	if err := gatewayclient.NewWithToken(u, a.gatewayTokenForURL(u)).PutGlobalPolicy(context.Background(), nil); err != nil {
		return err
	}
	fmt.Println("policy delete --global: ok")
	return nil
}

// PolicySet stores sandbox base policy; the gateway builds the effective
// candidate via composition (base + provider-composed profiles) — OpenShell-style.
//
// Requires the sandbox to be registered on the current gateway (create/register).
// Without a gateway, falls back to writing the file directly (no composition).
// When wait is true, blocks until the bind file matches and settle elapsed.
func (a *App) PolicySet(sandboxName, path string, wait bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := policy.Parse(b)
	if err != nil {
		return fmt.Errorf("policy set: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("policy set: %w", err)
	}

	var appliedHost string
	var appliedBytes []byte

	if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
		c := gatewayclient.NewWithToken(gw, a.gatewayTokenForURL(gw))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := c.Healthz(ctx); err == nil {
			eff, stripped, err := c.PutSandboxPolicy(ctx, sandboxName, b)
			if err != nil {
				return fmt.Errorf("policy set: %w", err)
			}
			if stripped > 0 {
				fmt.Printf("policy set: stripped %d provider-composed rule(s) from base (prefer policy get --base)\n", stripped)
			}
			effDoc, err := policy.Parse(eff)
			if err != nil {
				return fmt.Errorf("policy set: effective: %w", err)
			}
			var eng engine.Allowlist
			if err := eng.Apply(effDoc); err != nil {
				return fmt.Errorf("policy set: engine: %w", err)
			}
			if a.Docker != nil {
				hostPath, err := a.Docker.PolicyHostPath(ctx, sandboxName)
				if err != nil {
					return fmt.Errorf("policy set: gateway stored base, but live bind: %w (run: osg provider effective %s | …)", err, sandboxName)
				}
				if err := writeFileInPlace(hostPath, eff); err != nil {
					return fmt.Errorf("policy set: write %s: %w", hostPath, err)
				}
				appliedHost, appliedBytes = hostPath, eff
				fmt.Printf("policy set: ok sandbox=%s base stored; effective policy applied (rules=%d) host=%s\n",
					sandboxName, len(effDoc.AllowRules()), hostPath)
			} else {
				fmt.Printf("policy set: ok sandbox=%s base stored; effective policy ready (rules=%d, no docker bind)\n",
					sandboxName, len(effDoc.AllowRules()))
			}
			if wait {
				return a.waitPolicy(appliedHost, appliedBytes)
			}
			return nil
		}
	}

	// No gateway: overwrite bind file as-is (no composition).
	fmt.Fprintln(os.Stderr, "policy set: warn: no gateway — writing file directly (no provider composition)")
	doc, err = a.mergeGatewayGlobal(doc)
	if err != nil {
		return fmt.Errorf("policy set: %w", err)
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		return fmt.Errorf("policy set: engine: %w", err)
	}
	if a.Docker == nil {
		return fmt.Errorf("policy set: docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hostPath, err := a.Docker.PolicyHostPath(ctx, sandboxName)
	if err != nil {
		return err
	}
	if err := writeFileInPlace(hostPath, b); err != nil {
		return fmt.Errorf("policy set: write %s: %w", hostPath, err)
	}
	fmt.Printf("policy set: ok sandbox=%s host=%s allow_rules=%d (proxy reloads within ~1s)\n",
		sandboxName, hostPath, len(doc.AllowRules()))
	if wait {
		return a.waitPolicy(hostPath, b)
	}
	return nil
}

func (a *App) waitPolicy(hostPath string, want []byte) error {
	if hostPath == "" || len(want) == 0 {
		fmt.Println("policy set: --wait skipped (no live bind path)")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := policywait.FileApplied(ctx, hostPath, want, policywait.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return &ExitError{Code: policywait.ExitCode(err)}
	}
	fmt.Println("policy set: wait ok (file settled)")
	return nil
}

// PolicyUpdate applies incremental network changes to the sandbox base policy.
func (a *App) PolicyUpdate(sandbox string, endpoints, allows, denies, binaries []string, wait bool) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	baseYAML, err := c.GetSandboxPolicy(ctx, sandbox, "base")
	if err != nil {
		return err
	}
	doc, err := policy.Parse(baseYAML)
	if err != nil {
		return fmt.Errorf("policy update: parse base: %w", err)
	}
	u := policy.NetworkUpdate{Binaries: binaries}
	for _, s := range endpoints {
		ep, err := policy.ParseEndpointSpec(s)
		if err != nil {
			return err
		}
		u.AddEndpoints = append(u.AddEndpoints, ep)
	}
	for _, s := range allows {
		mp, err := policy.ParseMethodPathSpec(s)
		if err != nil {
			return err
		}
		u.AddAllows = append(u.AddAllows, mp)
	}
	for _, s := range denies {
		mp, err := policy.ParseMethodPathSpec(s)
		if err != nil {
			return err
		}
		u.AddDenies = append(u.AddDenies, mp)
	}
	out, err := policy.ApplyNetworkUpdate(doc, u)
	if err != nil {
		return err
	}
	raw, err := yamlMarshalDoc(out)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "osg-policy-update-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return a.PolicySet(sandbox, tmpPath, wait)
}

func yamlMarshalDoc(doc policy.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

// PolicyList prints policy revision metadata.
func (a *App) PolicyList(sandbox string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	revs, err := c.ListPolicyRevisions(context.Background(), sandbox)
	if err != nil {
		return err
	}
	if len(revs) == 0 {
		fmt.Println("policy list: (none)")
		return nil
	}
	for _, r := range revs {
		fmt.Printf("rev=%d status=%s bytes=%d at=%s\n", r.Rev, r.Status, r.Bytes, r.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

// PolicyGetRevision prints base YAML for a historical revision.
func (a *App) PolicyGetRevision(sandbox string, rev int) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	yamlBody, err := c.GetPolicyRevision(context.Background(), sandbox, rev)
	if err != nil {
		return err
	}
	fmt.Print(yamlBody)
	if len(yamlBody) > 0 && yamlBody[len(yamlBody)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

// SandboxProviderList prints attached provider metadata (no secret values).
func (a *App) SandboxProviderList(sandbox string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	list, err := c.ListSandboxProviders(context.Background(), sandbox)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("sandbox providers: (none)")
		return nil
	}
	for _, p := range list {
		fmt.Printf("%s\ttype=%s\tenv=%v\n", p.Name, p.Type, p.EnvVars)
	}
	return nil
}

// PolicyGet prints sandbox policy YAML. view is "base" or "full" (default full).
func (a *App) PolicyGet(sandboxName, view string) error {
	if view == "" {
		view = "full"
	}
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	b, err := c.GetSandboxPolicy(context.Background(), sandboxName, view)
	if err != nil {
		return err
	}
	fmt.Print(string(b))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

func writeFileInPlace(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func (a *App) mergeGatewayGlobal(doc policy.Document) (policy.Document, error) {
	u, err := a.currentGatewayURL()
	if err != nil || u == "" {
		return doc, nil
	}
	b, err := gatewayclient.NewWithToken(u, a.gatewayTokenForURL(u)).GetGlobalPolicy(context.Background())
	if err != nil || len(bytesTrim(b)) == 0 {
		return doc, nil
	}
	global, err := policy.Parse(b)
	if err != nil {
		return doc, fmt.Errorf("gateway global policy: %w", err)
	}
	return policy.MergeGlobal(doc, global)
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// InferenceList prints builtin provider presets.
func (a *App) InferenceList() error {
	return inference.ListBuiltins(os.Stdout)
}

// InferenceShow prints effective inference + network allow rules for a policy file.
func (a *App) InferenceShow(path string) error {
	if path == "" {
		path = "policies/default.yaml"
	}
	doc, err := policy.Load(path)
	if err != nil {
		return fmt.Errorf("inference show: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("inference show: %w", err)
	}
	return inference.ShowEffective(os.Stdout, doc)
}

// InferenceLocal prints a host-local model policy snippet.
func (a *App) InferenceLocal() error {
	return inference.WriteLocalSnippet(os.Stdout)
}

// SandboxCreateOpts are CLI flags for sandbox create.
type SandboxCreateOpts struct {
	Name           string
	Image          string
	Workspace      string
	Policy         string
	IKnow          bool
	NoProxy        bool
	NoHarden       bool
	Display        string // none | novnc
	DisplayPort    int
	OpenDisplay    bool
	Labels         map[string]string
	NoHostInternal bool // skip host.osg.internal ExtraHosts
	GatewayURL     string
	From           string // BYOC / community image alias
	SSH            bool
	NoVolume       bool // skip persist GuestData volume (default: persist)
	GPU            bool
	CDIDevices     []string
	Argv           []string // after `--`: create then exec (OpenShell-like)
	// Providers are profile ids or existing instance names (OpenShell --provider).
	// On create: discover host env, ensure gateway instance, compose policy before start, attach.
	Providers []string
	// AutoProviders enables OpenShell-style inference from trailing argv.
	AutoProviders bool
	// NoAutoProviders forces ModeOff even if AutoProviders is set.
	NoAutoProviders bool
	// Env is non-secret create-time environment (KEY=VALUE). Credential-looking keys warn.
	Env map[string]string
	// NoCredentialWarnings suppresses --env credential-looking key warnings.
	NoCredentialWarnings bool

	Detach   bool
	Editor   string // vscode | cursor
	Forwards []int
	Upload   string
	CPU      float64
	Memory   string
	Template string

	DriverConfigJSON string // --driver-config-json
	ApprovalMode     string // manual|auto
	NoKeep           bool   // delete sandbox after main command exits
	ForceTTY         bool   // --tty force PTY for create-time exec

	// Agent config injection (skills / MCP / AGENTS.md) — OpenShell-style /etc/osg.
	AgentConfig     string   // --agent-config path to agent-config.yaml
	Skills          []string // --skills PATH (repeatable)
	MCPCursor       string   // --mcp-cursor PATH → $HOME/.cursor/mcp.json
	MCPClaude       string   // --mcp-claude PATH → $HOME/.claude/mcp.json
	NoAgentConfig   bool     // --no-agent-config skip builtin + user inject
	Harness         string   // --harness cursor|claude
	RuntimeMode     string   // --runtime-mode once|watch
	AgentPrompt     string   // --agent-prompt PATH → agent-payload/agent-prompt.md
	CursorCLIConfig string   // --cursor-cli-config PATH → $HOME/.cursor/cli-config.json
}

// SandboxCreate creates and starts a sandbox.
func (a *App) SandboxCreate(opt SandboxCreateOpts) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox create: docker not available")
	}
	if opt.Template != "" {
		tpl, err := templates.Get(opt.Template)
		if err != nil {
			return fmt.Errorf("sandbox create template: %w", err)
		}
		mergeTemplateIntoCreate(&opt, tpl)
	}
	ws := opt.Workspace
	if ws == "" && a.GlobalWorkspace != "" && a.GlobalWorkspace != "default" {
		ws = a.GlobalWorkspace
	}
	if ws == "" {
		ws, _ = os.Getwd()
	}
	name := opt.Name
	if name == "" {
		name = filepath.Base(ws)
	}
	baseDoc, basePath, err := a.loadOrDenyAll(opt.Policy)
	if err != nil {
		return err
	}
	gwURL := opt.GatewayURL
	if gwURL == "" {
		// Production Docker path: gateway is required for secrets + registry unless --no-proxy
		// and no providers (dev escape hatch).
		needGW := !opt.NoProxy || len(opt.Providers) > 0 || opt.AutoProviders
		if err := a.GatewayEnsure(); err != nil {
			if needGW {
				return fmt.Errorf("sandbox create: %w", err)
			}
			fmt.Fprintf(os.Stderr, "sandbox create: warn: %v\n", err)
		}
		if cfg, _, err := gwconfig.Load(); err == nil {
			gwURL = gwconfig.CurrentURL(cfg)
		}
	}
	if gwURL == "" && (!opt.NoProxy || len(opt.Providers) > 0) {
		return fmt.Errorf("sandbox create: gateway required for proxy/providers (osg gateway ensure)")
	}
	if !opt.NoCredentialWarnings {
		hints := map[string][]env.ProfileHint{}
		for k := range opt.Env {
			if hs := env.HintKey(k); len(hs) > 0 {
				hints[k] = hs
			}
		}
		env.WarnCredentialEnv(os.Stderr, opt.Env, hints, false)
	}
	mode := autoprovider.ModeOff
	switch {
	case opt.NoAutoProviders:
		mode = autoprovider.ModeOff
	case opt.AutoProviders:
		mode = autoprovider.ModeOn
	default:
		// OpenShell: known trailing agents auto-create providers unless --no-auto-providers.
		if len(InferProvidersFromArgv(opt.Argv)) > 0 {
			mode = autoprovider.ModeOn
		}
	}
	opt.Providers = autoprovider.Merge(opt.Providers, InferProvidersFromArgv(opt.Argv), mode)
	attached, doc, policyPath, err := a.prepareProviders(baseDoc, basePath, opt.Providers, gwURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	img, err := gwconfig.ResolveImage(opt.From, opt.Image)
	if err != nil {
		return err
	}
	opt.Image = img

	if opt.Labels == nil {
		opt.Labels = map[string]string{}
	}
	if am := strings.ToLower(strings.TrimSpace(opt.ApprovalMode)); am != "" {
		if am != "manual" && am != "auto" {
			return fmt.Errorf("sandbox create: --approval-mode must be manual|auto")
		}
		opt.Labels["osg.approval-mode"] = am
	}

	spec := driver.Spec{
		Name:             name,
		Image:            opt.Image,
		Workspace:        ws,
		IKnow:            opt.IKnow,
		Env:              hostEnvForPolicy(doc),
		PolicyPath:       policyPath,
		NoHarden:         opt.NoHarden,
		Labels:           opt.Labels,
		PersistVolume:    !opt.NoVolume,
		EnableSSH:        opt.SSH,
		GPU:              opt.GPU || envTruthy("OSG_GPU"),
		CDIDevices:       append([]string{}, opt.CDIDevices...),
		CPU:              opt.CPU,
		DriverConfigJSON: opt.DriverConfigJSON,
	}
	if opt.Memory != "" {
		if mem, err := dockerdriver.ParseMemoryBytes(opt.Memory); err != nil {
			return fmt.Errorf("sandbox create --memory: %w", err)
		} else {
			spec.MemoryBytes = mem
		}
	}
	for _, p := range opt.Forwards {
		if p > 0 {
			spec.PublishPorts = append(spec.PublishPorts, driver.PortPublish{Host: p, Guest: p})
		}
	}
	for k, v := range opt.Env {
		spec.Env = append(spec.Env, k+"="+v)
	}
	if !opt.NoHostInternal {
		spec.ExtraHosts = dockerdriver.HostGatewayExtraHosts()
	}
	spec.GatewayURL = gwURL
	displayURL := ""
	displayPass := ""
	if display.ParseMode(opt.Display) == display.ModeNoVNC ||
		(doc.Display != nil && display.ParseMode(doc.Display.Mode) == display.ModeNoVNC) {
		pass, err := display.RandomPassword()
		if err != nil {
			return err
		}
		port := opt.DisplayPort
		if port <= 0 && doc.Display != nil && doc.Display.Port > 0 {
			port = doc.Display.Port
		}
		if port <= 0 {
			port = display.DefaultPort
		}
		spec.DisplayMode = "novnc"
		spec.DisplayPort = port
		spec.DisplayPassword = pass
		if spec.Image == "" {
			spec.Image = defaults.ImageGUI
		}
		displayPass = pass
		displayURL = display.NoVNCURL("127.0.0.1", port, pass)
	}
	if !opt.NoHarden {
		initBin, err := ensureInitBin(ctx)
		if err != nil {
			return err
		}
		spec.InitBin = initBin
	}
	if opt.SSH {
		mod, err := findModuleDir("github.com/zorneth/osg-runtime")
		if err != nil {
			return err
		}
		sshBin, err := sidecar.EnsureLinuxSSHD(ctx, mod)
		if err != nil {
			return err
		}
		spec.SSHBin = sshBin
		spec.EnableSSH = true
	}
	if !opt.NoProxy {
		bin, err := ensureProxyBin(ctx)
		if err != nil {
			return err
		}
		spec.ProxyBin = bin
		spec.ProxyEnv = append(proxySecretsForPolicy(doc), proxyGatewayEnv(opt.Name, gwURL)...)
	}
	h, err := a.Sandboxes.Create(ctx, sandbox.CreateOptions{
		Spec:   spec,
		Policy: doc,
	})
	if err != nil {
		return err
	}
	if gwURL != "" {
		cli := gatewayclient.New(gwURL)
		baseYAML := ""
		if b, err := os.ReadFile(basePath); err == nil {
			baseYAML = string(b)
		}
		_ = cli.UpsertSandbox(ctx, gatewayclient.Sandbox{
			Name:              h.Name,
			ID:                string(h.ID),
			Image:             h.Image,
			Network:           h.Network,
			Status:            "running",
			Labels:            opt.Labels,
			BasePolicyYAML:    baseYAML,
			AttachedProviders: attached,
		})
	}
	notes := []string{}
	if !opt.NoProxy {
		notes = append(notes, "proxy=sidecar")
	} else {
		notes = append(notes, "proxy=off")
	}
	if !opt.NoHarden {
		notes = append(notes, "harden=osg-init")
	} else {
		notes = append(notes, "harden=off")
	}
	if displayURL != "" {
		notes = append(notes, "display=novnc")
	}
	if spec.GPU {
		notes = append(notes, "gpu=cdi")
	}
	if gwURL != "" {
		notes = append(notes, "gateway=registered")
	}
	if len(opt.Providers) > 0 {
		notes = append(notes, "providers="+strings.Join(opt.Providers, ","))
	}
	fmt.Printf("sandbox create: ok name=%s id=%s network=%s image=%s %s\n",
		h.Name, shortID(string(h.ID)), h.Network, h.Image, strings.Join(notes, " "))
	ui.Ok("sandbox %s ready (%s)", h.Name, strings.Join(notes, " "))
	if displayURL != "" {
		fmt.Printf("display: %s\n", displayURL)
		fmt.Printf("display password: %s\n", displayPass)
		if opt.OpenDisplay {
			if err := display.OpenHostBrowser(displayURL); err != nil {
				fmt.Fprintf(os.Stderr, "display: open browser: %v\n", err)
			}
		}
	}
	if opt.SSH {
		if port, err := a.Sandboxes.Driver.SSHPort(ctx, h.ID); err == nil {
			fmt.Printf("ssh: enabled on 127.0.0.1:%d (osg connect --ssh %s)\n", port, h.Name)
		}
	}
	if spec.GPU {
		fmt.Printf("gpu: CDI DeviceRequests enabled (see docs/exp/GPU.md)\n")
	}
	if !opt.NoVolume {
		fmt.Printf("volume: osg-data-%s → %s (retained across stop/start)\n", h.Name, defaults.GuestData)
	}
	if opt.Upload != "" {
		dest := "/workspace/" + filepath.Base(opt.Upload)
		if err := mounts.ValidateUploadDest(dest); err != nil {
			return fmt.Errorf("sandbox create --upload: %w", err)
		}
		if err := a.Copy(opt.Upload, h.Name+":"+dest); err != nil {
			return err
		}
	}
	if err := a.installAgentConfig(h, opt); err != nil {
		return fmt.Errorf("sandbox create agent-config: %w", err)
	}
	for _, p := range opt.Forwards {
		if p > 0 {
			_ = a.ForwardStart(h.Name, fmt.Sprintf("%d", p), fmt.Sprintf("%d", p), true)
		}
	}
	switch strings.ToLower(strings.TrimSpace(opt.Editor)) {
	case "vscode", "cursor":
		if err := a.SandboxSSHConfig(h.Name); err != nil {
			fmt.Fprintf(os.Stderr, "editor: ssh-config: %v\n", err)
		} else {
			fmt.Printf("editor: open %s via Remote-SSH (Host osg-%s)\n", opt.Editor, h.Name)
		}
	}
	if len(opt.Argv) > 0 {
		if opt.Detach {
			fmt.Printf("sandbox create: --detach skipping exec of %v\n", opt.Argv)
			return nil
		}
		tty := opt.ForceTTY || term.IsTerminal(int(os.Stdin.Fd()))
		execErr := a.Exec(ExecOpts{
			Name: h.Name,
			Argv: opt.Argv,
			TTY:  tty,
			Env:  hostEnvForPolicy(doc),
		})
		if opt.NoKeep {
			_ = a.SandboxRemove(h.Name)
			fmt.Printf("sandbox create: --no-keep removed %s\n", h.Name)
		}
		return execErr
	}
	return nil
}

// SandboxList prints sandboxes.
func (a *App) SandboxList() error {
	list, err := a.ListSandboxes()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("sandbox list: (none)")
		return nil
	}
	fmt.Printf("%-16s %-12s %-20s %s\n", "NAME", "ID", "STATUS", "IMAGE")
	for _, s := range list {
		fmt.Printf("%-16s %-12s %-20s %s\n", s.Name, shortID(string(s.ID)), s.Status, s.Image)
	}
	return nil
}

// ListSandboxes returns sandbox infos for TUI / automation.
func (a *App) ListSandboxes() ([]driver.Info, error) {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return nil, fmt.Errorf("sandbox list: docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.Sandboxes.List(ctx)
}

// SandboxStatus prints one sandbox.
func (a *App) SandboxStatus(nameOrID string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox status: docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := a.Sandboxes.Driver.Inspect(ctx, nameOrID)
	if err != nil {
		return err
	}
	fmt.Printf("name:    %s\n", info.Name)
	fmt.Printf("id:      %s\n", info.ID)
	fmt.Printf("status:  %s\n", info.Status)
	fmt.Printf("network: %s\n", info.Network)
	fmt.Printf("image:   %s\n", info.Image)
	return nil
}

// SandboxRemove deletes a sandbox.
func (a *App) SandboxRemove(nameOrID string) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox rm: docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := nameOrID
	if info, err := a.Sandboxes.Driver.Inspect(ctx, nameOrID); err == nil && info.Name != "" {
		name = info.Name
	}
	if err := a.Sandboxes.Remove(ctx, nameOrID); err != nil {
		return err
	}
	if cfg, _, err := gwconfig.Load(); err == nil {
		if u := gwconfig.CurrentURL(cfg); u != "" {
			_ = gatewayclient.New(u).DeleteSandbox(ctx, name)
		}
	}
	fmt.Printf("sandbox rm: ok %s\n", nameOrID)
	return nil
}

// SandboxConnect opens an interactive shell (TTY) in the sandbox.
func (a *App) SandboxConnect(name string, argv []string) error {
	if len(argv) == 0 {
		argv = ui.DefaultShellArgv()
	}
	return a.Exec(ExecOpts{
		Name: name,
		Argv: argv,
		TTY:  term.IsTerminal(int(os.Stdin.Fd())),
	})
}

// LogsOpts configures observation log streaming.
type LogsOpts struct {
	Names  []string
	Follow bool
	All    bool
	Since  string
	Source string
	Level  string
}

// Logs streams sandbox container logs to stdout.
func (a *App) Logs(name string, follow bool) error {
	return a.LogsOpts(LogsOpts{Names: []string{name}, Follow: follow})
}

// LogsOpts streams observation logs (gateway SSE preferred; docker fallback).
func (a *App) LogsOpts(opt LogsOpts) error {
	return a.LogsToOpts(context.Background(), opt, os.Stdout)
}

// LogsTo streams sandbox container logs to w (used by `osg term` live observation panel).
func (a *App) LogsTo(ctx context.Context, name string, follow bool, w io.Writer) error {
	return a.LogsToOpts(ctx, LogsOpts{Names: []string{name}, Follow: follow}, w)
}

// LogsToOpts streams one or more sandboxes.
func (a *App) LogsToOpts(ctx context.Context, opt LogsOpts, w io.Writer) error {
	if w == nil {
		w = os.Stdout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	names := opt.Names
	if opt.All {
		list, err := a.ListSandboxes()
		if err == nil {
			names = nil
			for _, s := range list {
				names = append(names, s.Name)
			}
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("logs: no sandbox names")
	}
	// Prefer gateway SSE when available.
	if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
		c := gatewayclient.NewWithToken(gw, a.gatewayTokenForURL(gw))
		if _, err := c.Healthz(ctx); err == nil {
			if opt.Follow {
				return c.FollowLogs(ctx, names, opt.All, opt.Since, opt.Source, opt.Level, w)
			}
			// snapshot each; if gateway has nothing yet, fall through to docker
			var any bool
			for _, n := range names {
				lines, err := c.GetLogsSnapshot(ctx, n, opt.Since, opt.Source, opt.Level)
				if err != nil {
					continue
				}
				for _, ln := range lines {
					any = true
					prefix := n
					if len(names) == 1 {
						prefix = ln.Source
						if prefix == "" {
							prefix = "proxy"
						}
					}
					fmt.Fprintf(w, "[%s] %s\n", prefix, ln.Text)
				}
			}
			if any {
				return nil
			}
		}
	}
	// Docker fallback.
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("logs: docker not available")
	}
	if !opt.Follow {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	if len(names) > 1 {
		var mu sync.Mutex
		errCh := make(chan error, len(names))
		for _, n := range names {
			n := n
			go func() {
				pr, pw := io.Pipe()
				go func() {
					info, err := a.Sandboxes.Driver.Inspect(ctx, n)
					if err != nil {
						_ = pw.CloseWithError(err)
						return
					}
					err = a.Sandboxes.Driver.Logs(ctx, info.ID, opt.Follow, pw)
					_ = pw.CloseWithError(err)
				}()
				sc := bufio.NewScanner(pr)
				for sc.Scan() {
					mu.Lock()
					_, _ = fmt.Fprintf(w, "[%s] %s\n", n, sc.Text())
					mu.Unlock()
				}
				errCh <- sc.Err()
			}()
		}
		var first error
		for range names {
			if err := <-errCh; err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, names[0])
	if err != nil {
		return err
	}
	return a.Sandboxes.Driver.Logs(ctx, info.ID, opt.Follow, w)
}

// GatewayAdd registers a named gateway in config.
func (a *App) GatewayAdd(name, url string) error {
	return a.GatewayAddParsed(osargs.GatewayAdd{Name: name, Endpoint: url})
}

// GatewayAddParsed registers a gateway including optional OIDC metadata.
func (a *App) GatewayAddParsed(parsed osargs.GatewayAdd) error {
	name, url := parsed.Name, parsed.Endpoint
	if name == "" || url == "" {
		return fmt.Errorf("usage: osg gateway add <endpoint> [--name NAME] [--local] [--oidc-issuer URL]")
	}
	cfg, path, err := gwconfig.Load()
	if err != nil {
		return err
	}
	g := gwconfig.Gateway{
		URL:           url,
		OIDCIssuer:    parsed.OIDCIssuer,
		OIDCClientID:  parsed.OIDCClientID,
		OIDCAudience:  parsed.OIDCAudience,
		OIDCScopes:    parsed.OIDCScopes,
		OIDCAllowHTTP: parsed.OIDCAllowHTTP,
	}
	// Preserve tokens if re-adding same name with same URL.
	if prev, ok := cfg.Gateways[name]; ok && prev.URL == url {
		g.Token = prev.Token
		g.RefreshToken = prev.RefreshToken
		g.TokenExpiresAtMS = prev.TokenExpiresAtMS
		if g.OIDCIssuer == "" {
			g.OIDCIssuer = prev.OIDCIssuer
		}
		if g.OIDCClientID == "" {
			g.OIDCClientID = prev.OIDCClientID
		}
	}
	cfg.Gateways[name] = g
	cfg.Current = name
	if err := gwconfig.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("gateway add: ok name=%s url=%s config=%s\n", name, url, path)
	if g.OIDCIssuer != "" {
		fmt.Printf("gateway add: oidc issuer=%s client_id=%s\n", g.OIDCIssuer, g.OIDCClientID)
		fmt.Printf("gateway add: run `osg gateway login` for Authorization Code + PKCE\n")
	}
	return nil
}

// GatewaySelect sets the current gateway.
func (a *App) GatewaySelect(name string) error {
	cfg, path, err := gwconfig.Load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Gateways[name]; !ok {
		return fmt.Errorf("gateway select: unknown %q", name)
	}
	cfg.Current = name
	if err := gwconfig.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("gateway select: %s (%s)\n", name, path)
	return nil
}

// GatewayStatus prints local config + remote /healthz when reachable.
func (a *App) GatewayStatus() error {
	cfg, path, err := gwconfig.Load()
	if err != nil {
		return err
	}
	fmt.Printf("config: %s\n", path)
	fmt.Printf("current: %s\n", cfg.Current)
	for name, g := range cfg.Gateways {
		mark := " "
		if name == cfg.Current {
			mark = "*"
		}
		fmt.Printf("%s %s  %s\n", mark, name, g.URL)
	}
	if u := gwconfig.CurrentURL(cfg); u != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		info, err := gatewayclient.New(u).Healthz(ctx)
		if err != nil {
			fmt.Printf("healthz: unreachable (%v)\n", err)
			return nil
		}
		fmt.Printf("healthz: ok %+v\n", info)
	}
	return nil
}

// GatewayListRemote lists sandboxes registered on the current gateway.
func (a *App) GatewayListRemote() error {
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return err
	}
	u := gwconfig.CurrentURL(cfg)
	if u == "" {
		return fmt.Errorf("gateway list: no current gateway (osg gateway add …)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := gatewayclient.New(u).ListSandboxes(ctx)
	if err != nil {
		return err
	}
	for _, sb := range list {
		fmt.Printf("%s\tid=%s\tstatus=%s\timage=%s\n", sb.Name, shortID(sb.ID), sb.Status, sb.Image)
	}
	if len(list) == 0 {
		fmt.Println("(no sandboxes registered)")
	}
	return nil
}

// ExecOpts for osg sandbox exec.
type ExecOpts struct {
	Name    string
	Argv    []string
	TTY     bool
	Env     []string
	WorkDir string
}

// Exec runs a command in a sandbox.
func (a *App) Exec(opt ExecOpts) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("exec: docker not available")
	}
	if opt.Name == "" || len(opt.Argv) == 0 {
		return fmt.Errorf("usage: osg sandbox exec [--name] <name> [--workdir DIR] [--env K=V] -- CMD")
	}
	// Always overlay credential placeholders from effective policy so attach/refresh
	// works without recreating the container (Docker Config.Env is immutable).
	guestEnv := a.credentialPlaceholdersForSandbox(opt.Name)
	// git ignores SSL_CERT_FILE; older sandboxes lack GIT_SSL_CAINFO at create-time.
	guestEnv = mergeEnvEntries(guestEnv, sidecar.CABundleEnv(defaults.GuestCAFile))
	guestEnv = mergeEnvEntries(guestEnv, opt.Env)
	tty := opt.TTY
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	a.emitProc(opt.Name, "LAUNCH", strings.Join(opt.Argv, " "), 0)
	res, err := a.Sandboxes.Exec(ctx, opt.Name, driver.ExecRequest{
		Argv:    opt.Argv,
		TTY:     tty,
		Env:     guestEnv,
		WorkDir: opt.WorkDir,
	})
	code := 0
	if res.ExitCode != 0 {
		code = res.ExitCode
	}
	a.emitProc(opt.Name, "EXIT", strings.Join(opt.Argv, " "), code)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return &ExitError{Code: res.ExitCode}
	}
	return nil
}

func (a *App) emitProc(sandbox, activity, details string, exitCode int) {
	gw, err := a.currentGatewayURL()
	if err != nil || gw == "" || sandbox == "" {
		return
	}
	ts := time.Now().UTC()
	text := fmt.Sprintf("%s OCSF PROC:%s [INFO] ALLOWED %s", ts.Format(time.RFC3339Nano), activity, details)
	if activity == "EXIT" {
		text = fmt.Sprintf("%s OCSF PROC:EXIT [INFO] ALLOWED %s [exit:%d]", ts.Format(time.RFC3339Nano), details, exitCode)
	}
	c := gatewayclient.NewWithToken(gw, a.gatewayTokenForURL(gw))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.PostLogs(ctx, sandbox, []gatewayclient.LogLine{{
		TS: ts, Source: "proc", Level: "INFO", Text: text,
	}})
}

// RunOpts for osg run (ensure sandbox + exec).
type RunOpts struct {
	Name        string
	Image       string
	Workspace   string
	Policy      string
	IKnow       bool
	Argv        []string
	NoTTY       bool
	NoProxy     bool
	NoHarden    bool
	Display     string
	DisplayPort int
	OpenDisplay bool
}

// Run ensures a sandbox exists, then execs the command (default: bash).
func (a *App) Run(opt RunOpts) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("run: docker not available")
	}
	ws := opt.Workspace
	if ws == "" {
		ws, _ = os.Getwd()
	}
	name := opt.Name
	if name == "" {
		name = filepath.Base(ws)
	}
	doc, policyPath, err := a.loadOrDenyAll(opt.Policy)
	if err != nil {
		return err
	}
	guestEnv := hostEnvForPolicy(doc)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, err := a.Sandboxes.Driver.Inspect(ctx, name); err != nil {
		spec := driver.Spec{
			Name:          name,
			Image:         opt.Image,
			Workspace:     ws,
			IKnow:         opt.IKnow,
			Env:           guestEnv,
			PolicyPath:    policyPath,
			NoHarden:      opt.NoHarden,
			PersistVolume: true,
			ExtraHosts:    dockerdriver.HostGatewayExtraHosts(),
		}
		if display.ParseMode(opt.Display) == display.ModeNoVNC ||
			(doc.Display != nil && display.ParseMode(doc.Display.Mode) == display.ModeNoVNC) {
			pass, err := display.RandomPassword()
			if err != nil {
				return err
			}
			port := opt.DisplayPort
			if port <= 0 && doc.Display != nil && doc.Display.Port > 0 {
				port = doc.Display.Port
			}
			if port <= 0 {
				port = display.DefaultPort
			}
			spec.DisplayMode = "novnc"
			spec.DisplayPort = port
			spec.DisplayPassword = pass
			if spec.Image == "" {
				spec.Image = defaults.ImageGUI
			}
			url := display.NoVNCURL("127.0.0.1", port, pass)
			fmt.Fprintf(os.Stderr, "display: %s\n", url)
			fmt.Fprintf(os.Stderr, "display password: %s\n", pass)
			if opt.OpenDisplay {
				_ = display.OpenHostBrowser(url)
			}
		}
		if !opt.NoHarden {
			initBin, err := ensureInitBin(ctx)
			if err != nil {
				return err
			}
			spec.InitBin = initBin
		}
		if !opt.NoProxy {
			bin, err := ensureProxyBin(ctx)
			if err != nil {
				return err
			}
			spec.ProxyBin = bin
			spec.ProxyEnv = append(proxySecretsForPolicy(doc), proxyGatewayEnv(name, "")...)
		}
		h, err := a.Sandboxes.Create(ctx, sandbox.CreateOptions{
			Spec:   spec,
			Policy: doc,
		})
		if err != nil {
			return err
		}
		ui.Ok("sandbox %s created", h.Name)
		fmt.Fprintf(os.Stderr, "sandbox create: ok name=%s id=%s\n", h.Name, shortID(string(h.ID)))
	} else {
		info, _ := a.Sandboxes.Driver.Inspect(ctx, name)
		if info.Status != "" && info.Status != "running" {
			_ = a.Sandboxes.Driver.Start(ctx, info.ID)
		}
	}

	argv := opt.Argv
	if len(argv) == 0 {
		argv = ui.DefaultShellArgv()
	}
	tty := false
	if !opt.NoTTY {
		tty = term.IsTerminal(int(os.Stdin.Fd()))
	}
	return a.Exec(ExecOpts{
		Name: name,
		Argv: argv,
		TTY:  tty,
		Env:  guestEnv,
	})
}

// ProxyOpts for foreground host-side CONNECT proxy (debug / host-proxy mode).
type ProxyOpts struct {
	Listen     string
	Policy     string
	CAOut      string // write MITM CA PEM (for clients / sandbox trust)
	GatewayURL string // optional: pull secrets + push OCSF
	Sandbox    string // sandbox name for gateway resolve/push
	LogDir     string // optional daily OCSF file dir (default /var/log)
}

// Proxy runs osg-proxy until interrupted.
func (a *App) Proxy(opt ProxyOpts) error {
	listen := opt.Listen
	if listen == "" {
		listen = defaults.ProxyListenLocal()
	}
	doc, _, err := a.loadOrDenyAll(opt.Policy)
	if err != nil {
		return err
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		return err
	}
	logDir := opt.LogDir
	if logDir == "" {
		logDir = os.Getenv("OSG_LOG_DIR")
	}
	if logDir == "" {
		logDir = "/var/log"
	}
	gwURL := opt.GatewayURL
	if gwURL == "" {
		gwURL = os.Getenv("OSG_GATEWAY_URL")
	}
	sandbox := opt.Sandbox
	if sandbox == "" {
		sandbox = os.Getenv("OSG_SANDBOX")
	}
	var pusher proxy.LogPusher
	if gwURL != "" && sandbox != "" {
		pusher = gatewayAuditPusher{c: gatewayclient.New(gwURL)}
	}
	audit := proxy.NewMultiAudit(os.Stderr, logDir, sandbox, pusher)
	defer audit.Close()
	srv := proxy.NewServer(&eng, audit)

	// Prefer gateway-stored secrets; fall back to process env.
	// OpenShell-style: OSG_GATEWAY_URL must resolve via ExtraHosts
	// (host.osg.internal → host-gateway). No hostname guessing.
	if gwURL != "" && sandbox != "" {
		gwURL = GuestGatewayURL(gwURL)
		if err := refreshProxySecrets(srv, gwURL, sandbox); err != nil {
			fmt.Fprintf(os.Stderr, "osg proxy: gateway secrets: %v (using process env)\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "osg proxy: secrets loaded from gateway for sandbox %s\n", sandbox)
		}
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for range t.C {
				_ = refreshProxySecrets(srv, gwURL, sandbox)
			}
		}()
	}

	if ca := srv.CA(); ca != nil && opt.CAOut != "" {
		if err := ca.WriteBundle(opt.CAOut); err != nil {
			return fmt.Errorf("proxy ca-out: %w", err)
		}
		fmt.Fprintf(os.Stderr, "osg proxy: wrote MITM CA bundle to %s\n", opt.CAOut)
	}
	proxy.LifecycleReady(audit, "proxy ready on "+listen)
	fmt.Fprintf(os.Stderr, "osg proxy: listening on %s (allow_rules=%d mitm_ca=%v)\n", listen, len(doc.AllowRules()), srv.CA() != nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if path := strings.TrimSpace(opt.Policy); path != "" {
		go srv.WatchPolicy(ctx, path, time.Second)
		fmt.Fprintf(os.Stderr, "osg proxy: watching policy %s for hot-reload\n", path)
	}
	return srv.ListenAndServe(ctx, listen)
}

func refreshProxySecrets(srv *proxy.Server, gwURL, sandbox string) error {
	c := gatewayclient.New(gwURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := c.ResolveSecrets(ctx, sandbox)
	if err != nil {
		return err
	}
	// Merge with process env so non-provider keys still work.
	store := proxy.LoadSecretsFromEnviron(os.Environ())
	for k, v := range m {
		store[k] = v
	}
	// Mirror GitHub token aliases (policy/credential_keys may list both).
	if v := store["GITHUB_TOKEN"]; v != "" {
		if _, ok := store["GH_TOKEN"]; !ok {
			store["GH_TOKEN"] = v
		}
	}
	if v := store["GH_TOKEN"]; v != "" {
		if _, ok := store["GITHUB_TOKEN"]; !ok {
			store["GITHUB_TOKEN"] = v
		}
	}
	srv.SetSecrets(store)
	return nil
}

// GuestGatewayURL rewrites loopback (and legacy host.docker.internal) gateway URLs
// to host.osg.internal — the OpenShell-style host-gateway alias injected via ExtraHosts.
func GuestGatewayURL(gwURL string) string {
	u := strings.TrimSpace(gwURL)
	if u == "" {
		return u
	}
	u = strings.Replace(u, "127.0.0.1", "host.osg.internal", 1)
	u = strings.Replace(u, "localhost", "host.osg.internal", 1)
	u = strings.Replace(u, "host.docker.internal", "host.osg.internal", 1)
	return u
}

// gatewayAuditPusher adapts gatewayclient to proxy.LogPusher.
type gatewayAuditPusher struct {
	c *gatewayclient.Client
}

func (g gatewayAuditPusher) PostLogs(ctx context.Context, sandbox string, lines []proxy.AuditLine) error {
	if g.c == nil || len(lines) == 0 {
		return nil
	}
	out := make([]gatewayclient.LogLine, len(lines))
	for i, l := range lines {
		out[i] = gatewayclient.LogLine{TS: l.TS, Source: l.Source, Level: l.Level, Text: l.Text}
	}
	return g.c.PostLogs(ctx, sandbox, out)
}

func hostEnvForPolicy(doc policy.Document) []string {
	out := env.FromHostForGuest(doc.CredentialEnvKeys()...)
	// Keep guest agent installs on PATH even when login shells reset it.
	out = append(out,
		"HOME="+defaults.GuestHome,
		"PATH="+defaults.GuestPath,
	)
	return out
}

// credentialPlaceholdersForSandbox returns osg:resolve:env placeholders for the
// sandbox effective policy credential keys (and attached provider guest keys).
// Profiles with inject_env: false (Cursor) are skipped — Agent validates the key
// client-side and rejects placeholders.
func (a *App) credentialPlaceholdersForSandbox(sandbox string) []string {
	keys := a.sandboxCredentialKeys(sandbox)
	return env.FromHostForGuest(keys...)
}

func (a *App) sandboxCredentialKeys(sandbox string) []string {
	var keys []string
	seen := map[string]struct{}{}
	omit := map[string]struct{}{} // inject_env:false — never guest-inject
	add := func(list []string) {
		for _, k := range list {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if _, skip := omit[k]; skip {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	if c, err := a.gatewayClient(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if sb, err := c.GetSandbox(ctx, sandbox); err == nil {
			for _, name := range sb.AttachedProviders {
				rec, err := c.GetProvider(ctx, name)
				if err != nil {
					continue
				}
				prof, err := loadBuiltinProfile(rec.Type)
				if err != nil {
					// Unknown profile: fall back to instance env vars.
					add(rec.EnvVars)
					continue
				}
				for _, cred := range prof.Credentials {
					if cred.InjectEnv != nil && !*cred.InjectEnv {
						for _, k := range cred.EnvVars {
							omit[strings.TrimSpace(k)] = struct{}{}
						}
					}
				}
				guest := prof.GuestEnvKeys()
				if len(rec.EnvVars) > 0 {
					// Restrict to instance keys, still honoring inject_env:false.
					var filtered []string
					for _, k := range rec.EnvVars {
						if _, skip := omit[k]; skip {
							continue
						}
						filtered = append(filtered, k)
					}
					guest = filtered
				}
				add(guest)
			}
		}
		if b, err := c.EffectivePolicy(ctx, sandbox); err == nil {
			if doc, err := policy.Parse(b); err == nil {
				add(doc.CredentialEnvKeys())
			}
		}
	}
	return keys
}

func mergeEnvEntries(base, extra []string) []string {
	keys := map[string]int{}
	out := make([]string, 0, len(base)+len(extra))
	add := func(entry string) {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			return
		}
		if i, exists := keys[key]; exists {
			out[i] = entry
			return
		}
		keys[key] = len(out)
		out = append(out, entry)
	}
	for _, e := range base {
		add(e)
	}
	for _, e := range extra {
		add(e)
	}
	return out
}

func proxySecretsForPolicy(doc policy.Document) []string {
	out := env.SecretsFromHost(doc.CredentialEnvKeys()...)
	for _, k := range defaults.ProxyEnvKeys {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// proxyGatewayEnv adds OSG_GATEWAY_URL / OSG_SANDBOX so the sidecar can resolve
// encrypted provider secrets and push OCSF logs (OpenShell-like).
// When an inference route is configured, also inject OSG_INFERENCE_* for inference.local.
func proxyGatewayEnv(sandbox, gwURL string) []string {
	var out []string
	if gwURL == "" {
		if cfg, _, err := gwconfig.Load(); err == nil {
			gwURL = gwconfig.CurrentURL(cfg)
		}
	}
	if gwURL == "" {
		return out
	}
	guestGW := GuestGatewayURL(gwURL)
	out = append(out, "OSG_GATEWAY_URL="+guestGW)
	if sandbox != "" {
		out = append(out, "OSG_SANDBOX="+sandbox)
	}
	out = append(out, "OSG_LOG_DIR=/var/log")
	out = append(out, inferenceProxyEnv(gwURL)...)
	return out
}

func inferenceProxyEnv(gwURL string) []string {
	c := gatewayclient.New(gwURL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	route, err := c.GetInference(ctx)
	if err != nil || route.Provider == "" {
		return nil
	}
	out := []string{
		"OSG_INFERENCE_MODEL=" + route.Model,
		fmt.Sprintf("OSG_INFERENCE_TIMEOUT=%d", route.TimeoutSec),
	}
	if up := inferenceUpstreamForType(route.Provider, c, ctx); up != "" {
		out = append(out, "OSG_INFERENCE_UPSTREAM="+up)
	}
	rec, err := c.GetProvider(ctx, route.Provider)
	if err == nil && len(rec.EnvVars) > 0 {
		// Prefer first credential key from host env at create time (sidecar also refreshes via gateway).
		if v, ok := os.LookupEnv(rec.EnvVars[0]); ok && v != "" {
			out = append(out, "OSG_INFERENCE_API_KEY="+v)
		}
	}
	return out
}

func inferenceUpstreamForType(providerName string, c *gatewayclient.Client, ctx context.Context) string {
	rec, err := c.GetProvider(ctx, providerName)
	if err != nil {
		return ""
	}
	switch strings.ToLower(rec.Type) {
	case "nvidia":
		return "https://integrate.api.nvidia.com"
	case "openai", "codex", "deepinfra":
		return "https://api.openai.com"
	case "anthropic", "claude", "claude-code":
		return "https://api.anthropic.com"
	case "ollama":
		return "http://host.osg.internal:11434"
	default:
		return "https://api.openai.com"
	}
}

func (a *App) loadOrDenyAll(path string) (policy.Document, string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return policy.Document{}, "", err
		}
		doc, err := policy.Load(abs)
		if err != nil {
			return policy.Document{}, "", err
		}
		doc, err = a.mergeGatewayGlobal(doc)
		if err != nil {
			return policy.Document{}, "", err
		}
		if err := doc.Validate(); err != nil {
			return policy.Document{}, "", err
		}
		return doc, abs, nil
	}
	dir, err := os.MkdirTemp("", "osg-policy-*")
	if err != nil {
		return policy.Document{}, "", err
	}
	abs := filepath.Join(dir, "deny-all.yaml")
	const body = "version: 1\nnetwork:\n  default: deny\n"
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		return policy.Document{}, "", err
	}
	doc, err := policy.Parse([]byte(body))
	if err != nil {
		return policy.Document{}, "", err
	}
	return doc, abs, nil
}

// InitOpts for `osg init --agent …`.
type InitOpts struct {
	Agent string
	Dir   string
	Force bool
}

// Init writes a starter policy for a known agent.
func (a *App) Init(opt InitOpts) error {
	agent := strings.ToLower(strings.TrimSpace(opt.Agent))
	dir := opt.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var srcName string
	switch agent {
	case "cursor":
		srcName = "cursor.yaml"
	default:
		return fmt.Errorf("init: unknown agent %q (supported: cursor)", opt.Agent)
	}
	mod, err := findModuleDir("github.com/zorneth/osg-cli")
	if err != nil {
		return err
	}
	src := filepath.Join(mod, "policies", srcName)
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("init: read bundled policy: %w", err)
	}
	outDir := filepath.Join(absDir, "policies")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(outDir, srcName)
	if _, err := os.Stat(dst); err == nil && !opt.Force {
		return fmt.Errorf("init: %s already exists (use --force)", dst)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("init: wrote %s\n", dst)
	fmt.Printf("next: osg policy check %s\n", dst)
	fmt.Printf("      osg agent login %s\n", agent)
	return nil
}

// AgentLogin checks host env keys for a named agent (MVP: no OAuth).
func (a *App) AgentLogin(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cursor":
		keys := []string{"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"}
		fmt.Println("agent login cursor: checking host env (no tokens printed)")
		found := 0
		for _, k := range keys {
			if os.Getenv(k) != "" {
				fmt.Printf("  %s: set\n", k)
				found++
			} else {
				fmt.Printf("  %s: missing\n", k)
			}
		}
		if found == 0 {
			return fmt.Errorf("agent login: set at least one of %s", strings.Join(keys, ", "))
		}
		fmt.Println("ok: use --provider cursor (and --provider github if needed) on sandbox create")
		fmt.Println("docs: docs/CURSOR.md")
		return nil
	default:
		return fmt.Errorf("agent login: unknown agent %q (supported: cursor)", name)
	}
}

func ensureProxyBin(ctx context.Context) (string, error) {
	mod, err := findModuleDir("github.com/zorneth/osg-cli")
	if err != nil {
		return "", err
	}
	return sidecar.EnsureLinuxCLI(ctx, mod)
}

func ensureInitBin(ctx context.Context) (string, error) {
	mod, err := findModuleDir("github.com/zorneth/osg-runtime")
	if err != nil {
		return "", err
	}
	return sidecar.EnsureLinuxInit(ctx, mod)
}

func findModuleDir(modulePath string) (string, error) {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	want := "module " + modulePath
	short := strings.TrimPrefix(modulePath, "github.com/zorneth/")
	for _, start := range candidates {
		dir := start
		for i := 0; i < 8; i++ {
			gm := filepath.Join(dir, "go.mod")
			b, err := os.ReadFile(gm)
			if err == nil && strings.Contains(string(b), want) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		try := filepath.Join(start, short)
		if b, err := os.ReadFile(filepath.Join(try, "go.mod")); err == nil &&
			strings.Contains(string(b), want) {
			return try, nil
		}
	}
	return "", fmt.Errorf("cannot find module %s (run from agent-blocker)", modulePath)
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
