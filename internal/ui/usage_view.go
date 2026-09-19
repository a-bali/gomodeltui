package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/balia/gomodeltui/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func usageRefreshCmd(fetchers []usage.Fetcher) tea.Cmd {
	commands := make([]tea.Cmd, 0, len(fetchers))
	for _, fetcher := range fetchers {
		fetcher := fetcher
		commands = append(commands, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			snapshot, err := fetcher.Fetch(ctx)
			return usageMsg{provider: fetcher.Provider(), snapshot: snapshot, err: err}
		})
	}
	return tea.Batch(commands...)
}

func (m Model) renderUsageScreen() string {
	header := lipgloss.NewStyle().Bold(true).Render("Provider usage") + mutedStyle.Render("  r refresh  u/Esc back")
	if len(m.usageFetchers) == 0 {
		return strings.Join([]string{header, "", mutedStyle.Render("No usage account is available. Set OPENCODE_API_KEY or COMMANDCODE_COOKIE, or sign in to Codex locally.")}, "\n")
	}
	lines := []string{header, ""}
	for _, fetcher := range m.usageFetchers {
		provider := fetcher.Provider()
		if err := m.usageErrors[provider]; err != "" {
			lines = append(lines, errorStyle.Render(provider+": ")+mutedStyle.Render(err))
			continue
		}
		snapshot, ok := m.usageSnapshots[provider]
		if !ok {
			lines = append(lines, mutedStyle.Render(provider+": loading…"))
			continue
		}
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(snapshot.Provider)+mutedStyle.Render("  "+snapshot.Source))
		for _, window := range snapshot.Windows {
			reset := "reset unavailable"
			if !window.ResetsAt.IsZero() {
				reset = "resets " + window.ResetsAt.Local().Format("Mon 15:04")
			}
			lines = append(lines, fmt.Sprintf("  %-8s %6.1f%% used  %s", window.Label+":", window.Used, mutedStyle.Render(reset)))
		}
		if snapshot.Credits != "" {
			creditLine := snapshot.Credits
			if !snapshot.CreditResetAt.IsZero() {
				creditLine += "  resets " + snapshot.CreditResetAt.Local().Format("Mon 15:04")
			}
			lines = append(lines, "  "+mutedStyle.Render(creditLine))
		}
	}
	return strings.Join(lines, "\n")
}
