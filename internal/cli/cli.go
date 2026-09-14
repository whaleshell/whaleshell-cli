// Package cli is the thin cobra-facing layer for the osg binary.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/zorneth/osg-cli/internal/app"
	tuipkg "github.com/zorneth/osg-cli/internal/tui"
	"github.com/zorneth/osg-core/defaults"
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
	case "term":
		return runTerm(a, args[1:])
	case "inference":
		return runInference(a, args[1:])
	case "init":
		return runInit(a, args[1:])
	case "agent":
		return runAgent(a, args[1:])
	case "gateway":
		return runGateway(a, args[1:])
	case "logs":
		return runLogs(a, args[1:])
	case "connect":
		return runConnect(a, args[1:])
	case "cp":
		return runCp(a, args[1:])
	case "relay":
		return runRelay(a, args[1:])
	case "install":
		return runInstall(a, args[1:])
	case "provider":
		return runProvider(a, args[1:])
	default:
		return fmt.Errorf("unknown command %q (try: version|health|policy|sandbox|exec|run|proxy|term|inference|init|agent|gateway|logs|connect|cp|relay|install|provider)", args[0])
	}
}

func runProvider(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg provider profile|create|list|attach|detach|effective …")
	}
	switch args[0] {
	case "profile":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg provider profile list|show|import …")
		}
		switch args[1] {
		case "list", "ls":
			return a.ProviderProfileList()
		case "show":
			if len(args) < 3 {
				return fmt.Errorf("usage: osg provider profile show <id|path>")
			}
			return a.ProviderProfileShow(args[2])
		case "import":
			if len(args) < 3 {
				return fmt.Errorf("usage: osg provider profile import <file.yaml>")
			}
			return a.ProviderProfileImport(args[2])
		default:
			return fmt.Errorf("unknown profile subcommand %q", args[1])
		}
	case "create":
		// osg provider create NAME --from PROFILE [--env KEY,KEY]
		if len(args) < 2 {
			return fmt.Errorf("usage: osg provider create <name> --from <profile> [--env K1,K2]")
		}
		name := args[1]
		from := ""
		var envVars []string
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--from":
				i++
				if i >= len(args) {
					return fmt.Errorf("--from needs a value")
				}
				from = args[i]
			case "--env":
				i++
				if i >= len(args) {
					return fmt.Errorf("--env needs a value")
				}
				for _, k := range strings.Split(args[i], ",") {
					k = strings.TrimSpace(k)
					if k != "" {
						envVars = append(envVars, k)
					}
				}
			default:
				return fmt.Errorf("unknown flag %q", args[i])
			}
		}
		if from == "" {
			return fmt.Errorf("usage: osg provider create <name> --from <profile>")
		}
		return a.ProviderCreate(name, from, envVars)
	case "list", "ls":
		return a.ProviderList()
	case "attach":
		if len(args) < 3 {
			return fmt.Errorf("usage: osg provider attach <sandbox> <provider>")
		}
		return a.ProviderAttach(args[1], args[2])
	case "detach":
		if len(args) < 3 {
			return fmt.Errorf("usage: osg provider detach <sandbox> <provider>")
		}
		return a.ProviderDetach(args[1], args[2])
	case "effective":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg provider effective <sandbox>")
		}
		return a.ProviderEffective(args[1])
	default:
		return fmt.Errorf("unknown provider subcommand %q", args[0])
	}
}

func runInstall(a *app.App, args []string) error {
	opt := app.InstallOpts{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--force":
			opt.Force = true
		default:
			return fmt.Errorf("unknown flag %q (usage: osg install [--force])", args[i])
		}
	}
	return a.Install(opt)
}

func runGateway(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg gateway add|select|status|list")
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg gateway add <name> --url URL")
		}
		name := args[1]
		url := ""
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--url":
				i++
				if i >= len(args) {
					return fmt.Errorf("--url needs a value")
				}
				url = args[i]
			default:
				return fmt.Errorf("unknown flag %q", args[i])
			}
		}
		return a.GatewayAdd(name, url)
	case "select":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg gateway select <name>")
		}
		return a.GatewaySelect(args[1])
	case "status":
		return a.GatewayStatus()
	case "list", "ls":
		return a.GatewayListRemote()
	default:
		return fmt.Errorf("unknown gateway subcommand %q", args[0])
	}
}

func runLogs(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg logs <name> [--follow]")
	}
	name := args[0]
	follow := false
	for _, a := range args[1:] {
		if a == "--follow" || a == "-f" {
			follow = true
		}
	}
	return a.Logs(name, follow)
}

func runConnect(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg connect <name> [--ssh] [-- cmd...]")
	}
	sshMode := false
	var name string
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ssh":
			sshMode = true
		case "--":
			rest = args[i+1:]
			i = len(args)
		default:
			if name == "" && !strings.HasPrefix(args[i], "-") {
				name = args[i]
			} else {
				rest = append(rest, args[i])
			}
		}
	}
	if name == "" {
		return fmt.Errorf("usage: osg connect <name> [--ssh]")
	}
	if sshMode {
		return a.ConnectSSH(name, true)
	}
	return a.SandboxConnect(name, rest)
}

func runCp(a *app.App, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: osg cp <src> <dst>")
	}
	return a.Copy(args[0], args[1])
}

func runRelay(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg relay agent|exec …")
	}
	switch args[0] {
	case "agent":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg relay agent <sandbox> [--gateway URL]")
		}
		name := args[1]
		gw := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--gateway" {
				i++
				if i < len(args) {
					gw = args[i]
				}
			}
		}
		return a.StartRelayAgent(name, gw)
	case "exec":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg relay exec <sandbox> -- <cmd>...")
		}
		name := args[1]
		rest := args[2:]
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return fmt.Errorf("usage: osg relay exec <sandbox> -- <cmd>...")
		}
		return a.RelayExec(name, rest)
	default:
		return fmt.Errorf("unknown relay subcommand %q", args[0])
	}
}

func runInit(a *app.App, args []string) error {
	opt := app.InitOpts{Dir: "."}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--agent":
			i++
			if i >= len(args) {
				return fmt.Errorf("--agent needs a value (cursor)")
			}
			opt.Agent = args[i]
		case "--dir":
			i++
			if i >= len(args) {
				return fmt.Errorf("--dir needs a value")
			}
			opt.Dir = args[i]
		case "--force":
			opt.Force = true
		default:
			return fmt.Errorf("unknown flag %q (usage: osg init --agent cursor [--dir .] [--force])", args[i])
		}
	}
	if opt.Agent == "" {
		return fmt.Errorf("usage: osg init --agent cursor [--dir .] [--force]")
	}
	return a.Init(opt)
}

func runAgent(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg agent login <name>")
	}
	switch args[0] {
	case "login":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg agent login cursor")
		}
		return a.AgentLogin(args[1])
	default:
		return fmt.Errorf("unknown agent subcommand %q", args[0])
	}
}

func runInference(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg inference list|show|local [policy]")
	}
	switch args[0] {
	case "list", "ls":
		return a.InferenceList()
	case "show":
		path := ""
		if len(args) > 1 {
			path = args[1]
		}
		return a.InferenceShow(path)
	case "local":
		return a.InferenceLocal()
	default:
		return fmt.Errorf("unknown inference subcommand %q", args[0])
	}
}

func runPolicy(a *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: osg policy check|set|global …")
	}
	switch args[0] {
	case "check":
		path := "policies/default.yaml"
		if len(args) > 1 {
			path = args[1]
		}
		return a.PolicyCheck(path)
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: osg policy set <sandbox> <path>")
		}
		return a.PolicySet(args[1], args[2])
	case "global":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg policy global get|set <path>")
		}
		switch args[1] {
		case "get":
			return a.PolicyGlobalGet()
		case "set":
			if len(args) < 3 {
				return fmt.Errorf("usage: osg policy global set <path>")
			}
			return a.PolicyGlobalSet(args[2])
		default:
			return fmt.Errorf("unknown policy global subcommand %q", args[1])
		}
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
		opt := app.SandboxCreateOpts{Workspace: ".", Labels: map[string]string{}}
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == "--" {
				opt.Argv = append([]string{}, rest[i+1:]...)
				break
			}
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
			case "--no-harden":
				opt.NoHarden = true
			case "--display":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--display needs a value (none|novnc)")
				}
				opt.Display = rest[i]
			case "--display-port":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--display-port needs a value")
				}
				var p int
				if _, err := fmt.Sscanf(rest[i], "%d", &p); err != nil || p <= 0 {
					return fmt.Errorf("invalid --display-port")
				}
				opt.DisplayPort = p
			case "--open-display":
				opt.OpenDisplay = true
			case "--label":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--label needs key=value")
				}
				k, v, ok := strings.Cut(rest[i], "=")
				if !ok || k == "" {
					return fmt.Errorf("--label needs key=value")
				}
				opt.Labels[k] = v
			case "--gateway":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--gateway needs a URL")
				}
				opt.GatewayURL = rest[i]
			case "--from":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--from needs an alias")
				}
				opt.From = rest[i]
			case "--ssh":
				opt.SSH = true
			case "--gpu":
				opt.GPU = true
			case "--cdi":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--cdi needs a device id (e.g. nvidia.com/gpu=all)")
				}
				opt.CDIDevices = append(opt.CDIDevices, rest[i])
			case "--no-volume":
				opt.NoVolume = true
			case "--no-host-internal":
				opt.NoHostInternal = true
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
	case "stop":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg sandbox stop <name>")
		}
		return a.SandboxStop(args[1])
	case "start":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg sandbox start <name>")
		}
		return a.SandboxStart(args[1])
	case "rm", "remove", "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg sandbox rm <name>")
		}
		return a.SandboxRemove(args[1])
	case "connect":
		if len(args) < 2 {
			return fmt.Errorf("usage: osg sandbox connect <name> [--ssh]")
		}
		sshMode := false
		rest := args[2:]
		for _, f := range rest {
			if f == "--ssh" {
				sshMode = true
			}
		}
		if sshMode {
			return a.ConnectSSH(args[1], true)
		}
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		return a.SandboxConnect(args[1], rest)
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
		case "--no-harden":
			opt.NoHarden = true
		case "--display":
			i++
			if i >= len(args) {
				return fmt.Errorf("--display needs a value (none|novnc)")
			}
			opt.Display = args[i]
		case "--display-port":
			i++
			if i >= len(args) {
				return fmt.Errorf("--display-port needs a value")
			}
			var p int
			if _, err := fmt.Sscanf(args[i], "%d", &p); err != nil || p <= 0 {
				return fmt.Errorf("invalid --display-port")
			}
			opt.DisplayPort = p
		case "--open-display":
			opt.OpenDisplay = true
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
	opt := app.ProxyOpts{Listen: defaults.ProxyListenLocal()}
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
		case "--ca-out":
			i++
			if i >= len(args) {
				return fmt.Errorf("--ca-out needs a value")
			}
			opt.CAOut = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return a.Proxy(opt)
}

func runTerm(a *app.App, _ []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("osg term: requires an interactive terminal")
	}
	act, err := tuipkg.Run(a)
	if err != nil {
		return err
	}
	switch act.Kind {
	case "connect":
		return a.SandboxConnect(act.Name, nil)
	case "exec":
		return a.Exec(app.ExecOpts{Name: act.Name, Argv: []string{"bash"}, TTY: true})
	case "logs":
		return a.Logs(act.Name, false)
	default:
		return nil
	}
}

// Main is a helper for tests / embedding.
func Main() {
	if err := Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
