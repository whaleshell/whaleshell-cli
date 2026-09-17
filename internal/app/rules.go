// SPDX-FileCopyrightText: Copyright (c) 2026 zorneth
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zorneth/osg-sdk/gatewayclient"
)

// RuleListFilter lists policy.local proposals (gateway) with local fallback.
func (a *App) RuleListFilter(sandbox, status string) error {
	if sandbox != "" {
		if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
			c := gatewayclient.NewWithToken(gw, a.gatewayTokenForURL(gw))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			list, err := c.ListProposals(ctx, sandbox, status)
			if err == nil {
				b, _ := json.MarshalIndent(list, "", "  ")
				fmt.Println(string(b))
				return nil
			}
			fmt.Fprintf(os.Stderr, "rule: gateway list: %v (falling back to local)\n", err)
		}
	} else if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
		c := gatewayclient.NewWithToken(gw, a.gatewayTokenForURL(gw))
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var all []gatewayclient.Proposal
		for _, sb := range a.ruleCandidateSandboxes(ctx, c) {
			list, err := c.ListProposals(ctx, sb, status)
			if err != nil {
				continue
			}
			all = append(all, list...)
		}
		if len(all) > 0 {
			b, _ := json.MarshalIndent(all, "", "  ")
			fmt.Println(string(b))
			return nil
		}
	}
	return a.ruleListLocal(sandbox, status)
}

func (a *App) ruleListLocal(sandbox, status string) error {
	dir, err := localParityDir()
	if err != nil {
		return err
	}
	m, err := readJSONMap(filepath.Join(dir, "rules.json"))
	if err != nil {
		return err
	}
	filtered := map[string]any{}
	for id, raw := range m {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if sandbox != "" {
			if sb, _ := row["sandbox"].(string); sb != "" && sb != sandbox {
				continue
			}
		}
		if status != "" {
			st, _ := row["state"].(string)
			if st == "" {
				st, _ = row["status"].(string)
			}
			if st != status {
				continue
			}
		}
		filtered[id] = row
	}
	b, _ := json.MarshalIndent(filtered, "", "  ")
	fmt.Println(string(b))
	return nil
}

// RuleApprove approves a proposal, merges into sandbox base, writes live policy bind.
func (a *App) RuleApprove(id string) error {
	return a.ruleDecide(id, true, "")
}

// RuleRejectReason rejects a proposal with an optional reason.
func (a *App) RuleRejectReason(id, reason string) error {
	return a.ruleDecide(id, false, reason)
}

func (a *App) ruleDecide(id string, approve bool, reason string) error {
	gw, err := a.currentGatewayURL()
	if err != nil || gw == "" {
		state := "rejected"
		if approve {
			state = "approved"
		}
		return a.ruleSet(id, state, reason)
	}
	c := gatewayclient.NewWithToken(gw, a.gatewayTokenForURL(gw))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sandbox := ""
	found := false
	for _, sb := range a.ruleCandidateSandboxes(ctx, c) {
		pp, err := c.GetProposal(ctx, sb, id)
		if err == nil && pp.ID == id {
			sandbox = sb
			found = true
			break
		}
	}
	if !found {
		state := "rejected"
		if approve {
			state = "approved"
		}
		return a.ruleSet(id, state, reason)
	}

	if approve {
		p, err := c.ApproveProposal(ctx, sandbox, id)
		if err != nil {
			return err
		}
		eff, err := c.GetSandboxPolicy(ctx, sandbox, "full")
		if err != nil {
			return fmt.Errorf("rule approve: get effective: %w", err)
		}
		if a.Docker != nil {
			hostPath, err := a.Docker.PolicyHostPath(ctx, sandbox)
			if err != nil {
				return fmt.Errorf("rule approve: gateway ok, live bind: %w", err)
			}
			if err := writeFileInPlace(hostPath, eff); err != nil {
				return err
			}
			fmt.Printf("rule approve: ok id=%s sandbox=%s rule=%s policy=%s\n", id, sandbox, p.RuleName, hostPath)
		} else {
			fmt.Printf("rule approve: ok id=%s sandbox=%s (no docker bind)\n", id, sandbox)
		}
		return nil
	}
	if _, err := c.RejectProposal(ctx, sandbox, id, reason); err != nil {
		return err
	}
	fmt.Printf("rule reject: ok id=%s sandbox=%s reason=%q\n", id, sandbox, reason)
	return nil
}

func (a *App) ruleCandidateSandboxes(ctx context.Context, c *gatewayclient.Client) []string {
	var out []string
	if list, err := c.ListSandboxes(ctx); err == nil {
		for _, sb := range list {
			out = append(out, sb.Name)
		}
	}
	if len(out) == 0 && a.Sandboxes != nil {
		if infos, err := a.ListSandboxes(); err == nil {
			for _, i := range infos {
				out = append(out, i.Name)
			}
		}
	}
	return out
}
