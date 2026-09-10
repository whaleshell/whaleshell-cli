// Package app wires concrete runtime / display backends for the CLI.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lkmavi/osg-display"
	"github.com/lkmavi/osg-core/engine"
	"github.com/lkmavi/osg-core/policy"
	"github.com/lkmavi/osg-runtime/driver"
	"github.com/lkmavi/osg-runtime/driver/docker"
	"github.com/lkmavi/osg-runtime/env"
	"github.com/lkmavi/osg-runtime/proxy"
	"github.com/lkmavi/osg-runtime/sandbox"
	"github.com/lkmavi/osg-runtime/sidecar"
	"golang.org/x/term"
)

// App holds constructed dependencies for CLI commands.
type App struct {
	Sandboxes *sandbox.Manager
	Display   display.Stack
	Docker    *docker.Driver
}

// New builds the default host-side graph.
func New() *App {
	a := &App{Display: display.None{}}
	d, err := docker.New()
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

// Banner is the short CLI intro.
func (a *App) Banner() string {
	return "osg — agent sandbox CLI"
}

// Version reports the CLI stub version.
func (a *App) Version() string { return "osg 0.0.0-dev" }

// Health probes Docker Engine / Desktop.
func (a *App) Health() error {
	if a.Docker == nil {
		return fmt.Errorf("health: docker client unavailable (check DOCKER_HOST / Docker Desktop)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := a.Docker.Health(ctx)
	if !p.OK {
		return fmt.Errorf("health: docker unreachable: %s", p.Error)
	}
	fmt.Printf("health: ok\n")
	fmt.Printf("  docker.server:    %s\n", p.ServerVersion)
	fmt.Printf("  docker.api:       %s\n", p.APIVersion)
	fmt.Printf("  docker.os:        %s\n", p.OperatingSystem)
	fmt.Printf("  docker.arch:      %s\n", p.Architecture)
	fmt.Printf("  docker.context:   %s\n", p.Context)
	fmt.Printf("  isolation:        %s\n", p.Isolation)
	fmt.Printf("  host.goos:        %s\n", p.HostGOOS)
	fmt.Printf("  landlock:         (probe via osg-init helper — not wired yet)\n")
	fmt.Printf("  seccomp:          (probe via osg-init helper — not wired yet)\n")
	return nil
}

// PolicyCheck loads and validates a policy YAML file.
func (a *App) PolicyCheck(path string) error {
	doc, err := policy.Load(path)
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
	fmt.Printf("policy check: ok version=%d harden=%s allow_rules=%d include_workdir=%v\n",
		doc.Version, doc.HardenMode(), len(doc.AllowRules()),
		doc.Filesystem != nil && doc.Filesystem.IncludeWorkdir)
	return nil
}

// SandboxCreateOpts are CLI flags for sandbox create.
type SandboxCreateOpts struct {
	Name      string
	Image     string
	Workspace string
	Policy    string
	IKnow     bool
	NoProxy   bool
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

	spec := driver.Spec{
		Name:      name,
		Image:     opt.Image,
		Workspace: ws,
		IKnow:     opt.IKnow,
		Env:       hostEnvForPolicy(doc),
	}
	if !opt.NoProxy {
		bin, err := ensureProxyBin(ctx)
		if err != nil {
			return err
		}
		spec.ProxyBin = bin
		spec.PolicyPath = policyPath
	}
	h, err := a.Sandboxes.Create(ctx, sandbox.CreateOptions{
		Spec:   spec,
		Policy: doc,
	})
	if err != nil {
		return err
	}
	proxyNote := "proxy=off"
	if !opt.NoProxy {
		proxyNote = "proxy=sidecar"
	}
	fmt.Printf("sandbox create: ok name=%s id=%s network=%s image=%s %s\n",
		h.Name, shortID(string(h.ID)), h.Network, h.Image, proxyNote)
	return nil
}

// SandboxList prints sandboxes.
func (a *App) SandboxList() error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox list: docker not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	list, err := a.Sandboxes.List(ctx)
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
	if err := a.Sandboxes.Remove(ctx, nameOrID); err != nil {
		return err
	}
	fmt.Printf("sandbox rm: ok %s\n", nameOrID)
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
		return fmt.Errorf("exit code %d", res.ExitCode)
	}
	return nil
}

// RunOpts for osg run (ensure sandbox + exec).
type RunOpts struct {
	Name      string
	Image     string
	Workspace string
	Policy    string
	IKnow     bool
	Argv      []string
	NoTTY     bool
	NoProxy   bool
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
			Name:      name,
			Image:     opt.Image,
			Workspace: ws,
			IKnow:     opt.IKnow,
			Env:       guestEnv,
		}
		if !opt.NoProxy {
			bin, err := ensureProxyBin(ctx)
			if err != nil {
				return err
			}
			spec.ProxyBin = bin
			spec.PolicyPath = policyPath
		}
		h, err := a.Sandboxes.Create(ctx, sandbox.CreateOptions{
			Spec:   spec,
			Policy: doc,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "sandbox create: ok name=%s id=%s\n", h.Name, shortID(string(h.ID)))
	} else {
		info, _ := a.Sandboxes.Driver.Inspect(ctx, name)
		if info.Status != "" && info.Status != "running" {
			_ = a.Sandboxes.Driver.Start(ctx, info.ID)
		}
	}

	argv := opt.Argv
	if len(argv) == 0 {
		argv = []string{"bash"}
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
}

// Proxy runs osg-proxy until interrupted.
func (a *App) Proxy(opt ProxyOpts) error {
	listen := opt.Listen
	if listen == "" {
		listen = "127.0.0.1:3128"
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
	fmt.Fprintf(os.Stderr, "osg proxy: listening on %s (allow_rules=%d)\n", listen, len(doc.AllowRules()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return srv.ListenAndServe(ctx, listen)
}

func hostEnvForPolicy(doc policy.Document) []string {
	var extra []string
	if doc.Credentials != nil {
		extra = doc.Credentials.EnvAllow
	}
	return env.FromHost(extra...)
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

func ensureProxyBin(ctx context.Context) (string, error) {
	mod, err := findCLIModuleDir()
	if err != nil {
		return "", err
	}
	return sidecar.EnsureLinuxCLI(ctx, mod)
}

func findCLIModuleDir() (string, error) {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	for _, start := range candidates {
		dir := start
		for i := 0; i < 8; i++ {
			gm := filepath.Join(dir, "go.mod")
			b, err := os.ReadFile(gm)
			if err == nil && strings.Contains(string(b), "module github.com/lkmavi/osg-cli") {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		// org root / osg-cli
		try := filepath.Join(start, "osg-cli")
		if b, err := os.ReadFile(filepath.Join(try, "go.mod")); err == nil &&
			strings.Contains(string(b), "module github.com/lkmavi/osg-cli") {
			return try, nil
		}
	}
	return "", fmt.Errorf("cannot find osg-cli module (run from agent-blocker / osg-cli)")
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
