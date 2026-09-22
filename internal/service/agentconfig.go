// SPDX-FileCopyrightText: Copyright (c) 2026 whaleshell
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"os"

	core "github.com/whaleshell/whaleshell-core"
	"github.com/whaleshell/whaleshell-core/defaults"
	"github.com/whaleshell/whaleshell-driver/driver"
	"github.com/whaleshell/whaleshell-runtime/agentconfig"
)

// sandboxGuest adapts the compute driver for agentconfig.Install.
type sandboxGuest struct {
	drv driver.ComputeDriver
	id  core.ID
	ctx context.Context
}

func (g sandboxGuest) ExecRaw(argv []string) error {
	res, err := g.drv.Exec(g.ctx, g.id, driver.ExecRequest{Argv: argv, WorkDir: "/"})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("exit %d", res.ExitCode)
	}
	return nil
}

func (g sandboxGuest) CopyTo(hostSrc, guestDest string) error {
	return g.drv.CopyTo(g.ctx, g.id, hostSrc, guestDest)
}

func (a *App) installAgentConfig(h driver.Handle, opt SandboxCreateOpts) error {
	if opt.NoAgentConfig {
		return nil
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("agent-config: docker not available")
	}
	cfg := agentconfig.DefaultOptions()
	cfg.ManifestPath = opt.AgentConfig
	cfg.SkillPaths = append([]string{}, opt.Skills...)
	cfg.MCPCursor = opt.MCPCursor
	cfg.MCPClaude = opt.MCPClaude
	if opt.Harness != "" {
		cfg.Harness = opt.Harness
	}
	if opt.RuntimeMode != "" {
		cfg.RuntimeMode = opt.RuntimeMode
	}
	if opt.AgentPrompt != "" {
		cfg.PromptFile = opt.AgentPrompt
	}
	if opt.CursorCLIConfig != "" {
		cfg.CursorCLIConfig = opt.CursorCLIConfig
		cfg.SeedCursorCLI = true
	}

	st, err := agentconfig.Stage(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(st.Dir) }()

	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	g := sandboxGuest{drv: a.Sandboxes.Driver, id: h.ID, ctx: ctx}
	if err := agentconfig.Install(g, st); err != nil {
		return err
	}

	fmt.Printf("agent-config: %s + %s", defaults.GuestSkills, defaults.GuestAgentPayload)
	if st.MCPCursorHost != "" || st.MCPClaudeHost != "" {
		fmt.Printf(" + mcp")
	}
	if st.CLIConfigHost != "" {
		fmt.Printf(" + cli-config(attribution=off)")
	}
	if len(st.HomeSeeds) > 0 {
		fmt.Printf(" + home seeds")
	}
	fmt.Printf(" (HOME→%s, harness=%s/%s", defaults.GuestSandboxHome, st.Harness, st.RuntimeMode)
	if st.AgentsMDHost != "" {
		fmt.Printf("; /AGENTS.md")
	}
	fmt.Printf(")\n")
	fmt.Printf("agent-config: run supervisor: whaleshell sandbox exec %s -- %s/runtime/entrypoint.sh\n",
		h.Name, defaults.GuestAgentPayload)
	return nil
}
