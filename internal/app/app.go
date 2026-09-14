// Package app wires concrete runtime / display backends for the CLI.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zorneth/osg-cli/internal/gwconfig"
	"github.com/zorneth/osg-cli/internal/ui"
	"github.com/zorneth/osg-core/defaults"
	"github.com/zorneth/osg-core/engine"
	"github.com/zorneth/osg-core/policy"
	"github.com/zorneth/osg-display"
	"github.com/zorneth/osg-runtime/driver"
	"github.com/zorneth/osg-runtime/driver/docker"
	"github.com/zorneth/osg-runtime/driver/kubernetes"
	"github.com/zorneth/osg-runtime/driver/podman"
	"github.com/zorneth/osg-runtime/driver/vm"
	"github.com/zorneth/osg-runtime/env"
	"github.com/zorneth/osg-runtime/gatewayclient"
	"github.com/zorneth/osg-runtime/inference"
	"github.com/zorneth/osg-runtime/proxy"
	"github.com/zorneth/osg-runtime/sandbox"
	"github.com/zorneth/osg-runtime/sidecar"
	"golang.org/x/term"
)

// App holds constructed dependencies for CLI commands.
type App struct {
	Sandboxes  *sandbox.Manager
	Display    display.Stack
	Docker     *docker.Driver // Engine API client (Docker or Podman)
	DriverName string         // "docker" | "podman" | "vm" | "kubernetes"
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
	var d *docker.Driver
	var err error
	switch a.DriverName {
	case "podman":
		d, err = podman.New()
	default:
		d, err = docker.New()
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
func (a *App) Version() string { return "osg 0.0.0-dev" }

// Health probes Docker Engine / Podman API.
func (a *App) Health() error {
	if a.Docker == nil {
		hint := "check DOCKER_HOST / Docker Desktop"
		switch a.DriverName {
		case "podman":
			hint = "check OSG_PODMAN_SOCKET / podman.socket (systemctl --user start podman.socket)"
		case "vm":
			return fmt.Errorf("health: vm driver is a spike stub (see docs/MICROVM.md)")
		case "kubernetes":
			return fmt.Errorf("health: kubernetes driver is a spike stub (see docs/KUBERNETES.md)")
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
	fmt.Printf("  seccomp:          %s\n", docker.SeccompNote())
	if envTruthy("OSG_LANDLOCK_REQUIRED") && !landlockABIAtLeast(probe, 1) {
		return fmt.Errorf("health: landlock gate failed (need ABI≥1; got %s). Unset OSG_LANDLOCK_REQUIRED on Docker Desktop / ABI 0 hosts", probe)
	}
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
	doc, err = mergeGatewayGlobal(doc)
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
		doc.Filesystem != nil && doc.Filesystem.IncludeWorkdir)
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
	u, err := currentGatewayURL()
	if err != nil {
		return err
	}
	b, err := gatewayclient.New(u).GetGlobalPolicy(context.Background())
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
	u, err := currentGatewayURL()
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
	if err := gatewayclient.New(u).PutGlobalPolicy(context.Background(), b); err != nil {
		return err
	}
	fmt.Printf("policy global set: ok url=%s bytes=%d\n", u, len(b))
	return nil
}

// PolicySet writes a new policy YAML onto the host bind used by sandbox+proxy.
// The proxy watches the file and hot-reloads (in-place overwrite keeps the bind inode).
func (a *App) PolicySet(sandboxName, path string) error {
	if a.Docker == nil {
		return fmt.Errorf("policy set: docker not available")
	}
	doc, err := policy.Load(path)
	if err != nil {
		return fmt.Errorf("policy set: %w", err)
	}
	doc, err = mergeGatewayGlobal(doc)
	if err != nil {
		return fmt.Errorf("policy set: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("policy set: %w", err)
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		return fmt.Errorf("policy set: engine: %w", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
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

func currentGatewayURL() (string, error) {
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return "", err
	}
	u := gwconfig.CurrentURL(cfg)
	if u == "" {
		return "", fmt.Errorf("no current gateway; run: osg gateway add|select")
	}
	return u, nil
}

func mergeGatewayGlobal(doc policy.Document) (policy.Document, error) {
	cfg, _, err := gwconfig.Load()
	if err != nil || gwconfig.CurrentURL(cfg) == "" {
		return doc, nil
	}
	b, err := gatewayclient.New(gwconfig.CurrentURL(cfg)).GetGlobalPolicy(context.Background())
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
}

// SandboxCreate creates and starts a sandbox.
func (a *App) SandboxCreate(opt SandboxCreateOpts) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox create: docker not available")
	}
	ws := opt.Workspace
	if ws == "" {
		ws, _ = os.Getwd()
	}
	name := opt.Name
	if name == "" {
		name = filepath.Base(ws)
	}
	doc, policyPath, err := loadOrDenyAll(opt.Policy)
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

	spec := driver.Spec{
		Name:          name,
		Image:         opt.Image,
		Workspace:     ws,
		IKnow:         opt.IKnow,
		Env:           hostEnvForPolicy(doc),
		PolicyPath:    policyPath,
		NoHarden:      opt.NoHarden,
		Labels:        opt.Labels,
		PersistVolume: !opt.NoVolume,
		EnableSSH:     opt.SSH,
		GPU:           opt.GPU || envTruthy("OSG_GPU"),
		CDIDevices:    append([]string{}, opt.CDIDevices...),
	}
	if !opt.NoHostInternal {
		spec.ExtraHosts = []string{"host.osg.internal:host-gateway"}
	}
	gwURL := opt.GatewayURL
	if gwURL == "" {
		if cfg, _, err := gwconfig.Load(); err == nil {
			gwURL = gwconfig.CurrentURL(cfg)
		}
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
		spec.ProxyEnv = proxySecretsForPolicy(doc)
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
		if b, err := os.ReadFile(policyPath); err == nil {
			baseYAML = string(b)
		}
		_ = cli.UpsertSandbox(ctx, gatewayclient.Sandbox{
			Name:           h.Name,
			ID:             string(h.ID),
			Image:          h.Image,
			Network:        h.Network,
			Status:         "running",
			Labels:         opt.Labels,
			BasePolicyYAML: baseYAML,
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
		fmt.Printf("gpu: CDI DeviceRequests enabled (see docs/GPU.md)\n")
	}
	if !opt.NoVolume {
		fmt.Printf("volume: osg-data-%s → %s (retained across stop/start)\n", h.Name, defaults.GuestData)
	}
	if len(opt.Argv) > 0 {
		return a.Exec(ExecOpts{
			Name: h.Name,
			Argv: opt.Argv,
			TTY:  term.IsTerminal(int(os.Stdin.Fd())),
			Env:  hostEnvForPolicy(doc),
		})
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

// Logs streams sandbox container logs.
func (a *App) Logs(name string, follow bool) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("logs: docker not available")
	}
	ctx := context.Background()
	if !follow {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return err
	}
	return a.Sandboxes.Driver.Logs(ctx, info.ID, follow, os.Stdout)
}

// GatewayAdd registers a named gateway in config.
func (a *App) GatewayAdd(name, url string) error {
	if name == "" || url == "" {
		return fmt.Errorf("usage: osg gateway add <name> --url URL")
	}
	cfg, path, err := gwconfig.Load()
	if err != nil {
		return err
	}
	cfg.Gateways[name] = gwconfig.Gateway{URL: url}
	if cfg.Current == "" {
		cfg.Current = name
	}
	if err := gwconfig.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("gateway add: ok name=%s url=%s config=%s\n", name, url, path)
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

// ExecOpts for osg exec / run.
type ExecOpts struct {
	Name string
	Argv []string
	TTY  bool
	Env  []string
}

// Exec runs a command in a sandbox.
func (a *App) Exec(opt ExecOpts) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("exec: docker not available")
	}
	if opt.Name == "" || len(opt.Argv) == 0 {
		return fmt.Errorf("usage: osg exec <name> -- <cmd>...")
	}
	guestEnv := opt.Env
	if guestEnv == nil {
		guestEnv = env.FromHost()
	}
	tty := opt.TTY
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res, err := a.Sandboxes.Exec(ctx, opt.Name, driver.ExecRequest{
		Argv: opt.Argv,
		TTY:  tty,
		Env:  guestEnv,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return &ExitError{Code: res.ExitCode}
	}
	return nil
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
	doc, policyPath, err := loadOrDenyAll(opt.Policy)
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
			ExtraHosts:    []string{"host.osg.internal:host-gateway"},
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
			spec.ProxyEnv = proxySecretsForPolicy(doc)
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
	Listen string
	Policy string
	CAOut  string // write MITM CA PEM (for clients / sandbox trust)
}

// Proxy runs osg-proxy until interrupted.
func (a *App) Proxy(opt ProxyOpts) error {
	listen := opt.Listen
	if listen == "" {
		listen = defaults.ProxyListenLocal()
	}
	doc, _, err := loadOrDenyAll(opt.Policy)
	if err != nil {
		return err
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		return err
	}
	srv := proxy.NewServer(&eng, os.Stderr)
	if ca := srv.CA(); ca != nil && opt.CAOut != "" {
		if err := ca.WriteBundle(opt.CAOut); err != nil {
			return fmt.Errorf("proxy ca-out: %w", err)
		}
		fmt.Fprintf(os.Stderr, "osg proxy: wrote MITM CA bundle to %s\n", opt.CAOut)
	}
	fmt.Fprintf(os.Stderr, "osg proxy: listening on %s (allow_rules=%d mitm_ca=%v)\n", listen, len(doc.AllowRules()), srv.CA() != nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if path := strings.TrimSpace(opt.Policy); path != "" {
		go srv.WatchPolicy(ctx, path, time.Second)
		fmt.Fprintf(os.Stderr, "osg proxy: watching policy %s for hot-reload\n", path)
	}
	return srv.ListenAndServe(ctx, listen)
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

func proxySecretsForPolicy(doc policy.Document) []string {
	out := env.SecretsFromHost(doc.CredentialEnvKeys()...)
	for _, k := range defaults.ProxyEnvKeys {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func loadOrDenyAll(path string) (policy.Document, string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return policy.Document{}, "", err
		}
		doc, err := policy.Load(abs)
		if err != nil {
			return policy.Document{}, "", err
		}
		doc, err = mergeGatewayGlobal(doc)
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
		fmt.Println("ok: keys will be injectable when listed in credentials.env_allow")
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
