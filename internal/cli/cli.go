// Package cli is the thin cobra-facing layer for the osg binary.
package cli

import (
	"fmt"
	"os"

	"github.com/lkmavi/osg-cli/internal/app"
	"golang.org/x/term"
)

// Execute runs the CLI (stub root until full cobra wiring).
func Execute(args []string) error {
	a := app.New()
	if len(args) == 0 {
		fmt.Println(a.Banner())
		return nil
	}
	switch args[0] {
	case "version":
		fmt.Println(a.Version())
		return nil
	case "health":
		return a.Health()
	case "policy":
		return runPolicy(a, args[1:])
	case "sandbox":
		return runSandbox(a, args[1:])
	case "exec":
		return runExec(a, args[1:])
	case "run":
		return runRun(a, args[1:])
	case "proxy":
		return runProxy(a, args[1:])
	default:
		return fmt.Errorf("unknown command %q (try: version|health|policy|sandbox|exec|run|proxy)", args[0])
	}
}

func runPolicy(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg policy check [path]")
	}
	switch args[0] {
	case "check":
		path := "policies/default.yaml"
		if len(args) > 1 {
			path = args[1]
		}
		return a.PolicyCheck(path)
	default:
		return fmt.Errorf("unknown policy subcommand %q", args[0])
	}
}

func runSandbox(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg sandbox create|list|status|rm")
	}
	switch args[0] {
	case "create":
		opt := app.SandboxCreateOpts{Workspace: "."}
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--name":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--name needs a value")
				}
				opt.Name = rest[i]
			case "--image":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--image needs a value")
				}
				opt.Image = rest[i]
			case "--workspace":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--workspace needs a value")
				}
				opt.Workspace = rest[i]
			case "--policy":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--policy needs a value")
				}
				opt.Policy = rest[i]
			case "--i-know":
				opt.IKnow = true
			case "--no-proxy":
				opt.NoProxy = true
			default:
				return fmt.Errorf("unknown flag %q", rest[i])
			}
		}
		return a.SandboxCreate(opt)
	case "list", "ls":
		return a.SandboxList()
	case "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg sandbox status <name>")
		}
		return a.SandboxStatus(args[1])
	case "rm", "remove", "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg sandbox rm <name>")
		}
		return a.SandboxRemove(args[1])
	default:
		return fmt.Errorf("unknown sandbox subcommand %q", args[0])
	}
}

func runExec(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg exec <name> -- <cmd>...")
	}
	name := args[0]
	rest := args[1:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: osg exec <name> -- <cmd>...")
	}
	return a.Exec(app.ExecOpts{
		Name: name,
		Argv: rest,
		TTY:  term.IsTerminal(int(os.Stdin.Fd())),
	})
}

func runRun(a *app.App, args []string) error {
	opt := app.RunOpts{Workspace: "."}
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--name":
			i++
			if i >= len(args) {
				return fmt.Errorf("--name needs a value")
			}
			opt.Name = args[i]
		case "--image":
			i++
			if i >= len(args) {
				return fmt.Errorf("--image needs a value")
			}
			opt.Image = args[i]
		case "--workspace":
			i++
			if i >= len(args) {
				return fmt.Errorf("--workspace needs a value")
			}
			opt.Workspace = args[i]
		case "--policy":
			i++
			if i >= len(args) {
				return fmt.Errorf("--policy needs a value")
			}
			opt.Policy = args[i]
		case "--i-know":
			opt.IKnow = true
		case "--no-tty":
			opt.NoTTY = true
		case "--no-proxy":
			opt.NoProxy = true
		case "--":
			i++
			opt.Argv = args[i:]
			return a.Run(opt)
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				return fmt.Errorf("unknown flag %q", args[i])
			}
			opt.Argv = args[i:]
			return a.Run(opt)
		}
		i++
	}
	return a.Run(opt)
}

func runProxy(a *app.App, args []string) error {
	opt := app.ProxyOpts{Listen: "127.0.0.1:3128"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			i++
			if i >= len(args) {
				return fmt.Errorf("--listen needs a value")
			}
			opt.Listen = args[i]
		case "--policy":
			i++
			if i >= len(args) {
				return fmt.Errorf("--policy needs a value")
			}
			opt.Policy = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return a.Proxy(opt)
}

// Main is a helper for tests / embedding.
func Main() {
	if err := Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
