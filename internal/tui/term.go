// Package tui implements `osg term` — a small k9s-like sandbox browser.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zorneth/osg-cli/internal/app"
	"github.com/zorneth/osg-runtime/driver"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("33")).Bold(true)
	normalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// Action is a post-TUI command.
type Action struct {
	Kind string // connect | exec | logs | ""
	Name string
}

type model struct {
	app      *app.App
	items    []driver.Info
	cursor   int
	err      string
	status   string
	action   Action
	quitting bool
}

type refreshMsg struct {
	items []driver.Info
	err   error
}

// Run starts the interactive terminal UI and returns the chosen action (if any).
func Run(a *app.App) (Action, error) {
	m := model{app: a}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return Action{}, err
	}
	out, _ := final.(model)
	return out.action, nil
}

func (m model) Init() tea.Cmd {
	return refreshCmd(m.app)
}

func refreshCmd(a *app.App) tea.Cmd {
	return func() tea.Msg {
		list, err := a.ListSandboxes()
		return refreshMsg{items: list, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "r":
			m.status = "refreshing…"
			return m, refreshCmd(m.app)
		case "enter", "c":
			if len(m.items) == 0 {
				return m, nil
			}
			m.action = Action{Kind: "connect", Name: m.items[m.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		case "l":
			if len(m.items) == 0 {
				return m, nil
			}
			m.action = Action{Kind: "logs", Name: m.items[m.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		case "e":
			if len(m.items) == 0 {
				return m, nil
			}
			m.action = Action{Kind: "exec", Name: m.items[m.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		}
	case refreshMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.items = nil
		} else {
			m.err = ""
			m.items = msg.items
			if m.cursor >= len(m.items) && m.cursor > 0 {
				m.cursor = len(m.items) - 1
			}
			m.status = fmt.Sprintf("%d sandboxes", len(m.items))
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("osg term") + "  " + statusStyle.Render(m.status) + "\n\n")
	if m.err != "" {
		b.WriteString("error: " + m.err + "\n")
	}
	if len(m.items) == 0 {
		b.WriteString(normalStyle.Render("(no sandboxes)") + "\n")
	}
	for i, it := range m.items {
		line := fmt.Sprintf("%-16s %-10s %s", it.Name, it.Status, it.Image)
		if i == m.cursor {
			b.WriteString(selStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString(normalStyle.Render("  "+line) + "\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render("↑/↓ j/k  enter/c connect  e exec  l logs  r refresh  q quit"))
	b.WriteString("\n")
	return b.String()
}
