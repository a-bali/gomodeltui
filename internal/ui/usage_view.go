package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/a-bali/gomodeltui/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const usageProgressMinWidth = 12

func usageRefreshCmd(fetchers []usage.Fetcher, request uint64) tea.Cmd {
	commands := make([]tea.Cmd, 0, len(fetchers))
	for _, fetcher := range fetchers {
		fetcher := fetcher
		commands = append(commands, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			snapshot, err := fetcher.Fetch(ctx)
			return usageMsg{provider: fetcher.Provider(), snapshot: snapshot, err: err, request: request}
		})
	}
	return tea.Batch(commands...)
}

func (m Model) renderUsageScreen() string {
	header := lipgloss.NewStyle().Bold(true).Render("Provider usage") + mutedStyle.Render("  r refresh  u/Esc back")
	if len(m.usageFetchers) == 0 {
		return strings.Join([]string{header, "", mutedStyle.Render("No usage account is available. Set PROVIDERS_OPENCODE_API_KEY or PROVIDERS_COMMANDCODE_COOKIE, or sign in to Codex locally.")}, "\n")
	}
	now := time.Now()
	progressWidth := m.sharedUsageProgressWidth(now)
	lines := []string{header, ""}
	for _, fetcher := range m.usageFetchers {
		provider := fetcher.Provider()
		if m.usagePending > 0 {
			lines = append(lines, mutedStyle.Render(provider+": refreshing…"))
			continue
		}
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
			lines = append(lines, renderUsageWindow(m.width, window.Label, window.Used, usageWindowReset(window, now), progressWidth))
		}
		if snapshot.Credits != "" {
			creditLine := snapshot.Credits
			if !snapshot.CreditResetAt.IsZero() {
				creditLine += "  " + formatUsageReset(snapshot.CreditResetAt, time.Now())
			}
			lines = append(lines, "  "+mutedStyle.Render(creditLine))
		}
	}
	return strings.Join(lines, "\n")
}

// sharedUsageProgressWidth finds one bar width that fits every visible quota
// row, keeping progress indicators consistently sized across providers.
func (m Model) sharedUsageProgressWidth(now time.Time) int {
	shared := 0
	for _, fetcher := range m.usageFetchers {
		snapshot, ok := m.usageSnapshots[fetcher.Provider()]
		if !ok || m.usageErrors[fetcher.Provider()] != "" {
			continue
		}
		for _, window := range snapshot.Windows {
			available := usageProgressWidth(m.width, window.Label, usageWindowReset(window, now))
			if shared == 0 || available < shared {
				shared = available
			}
		}
	}
	if shared < usageProgressMinWidth {
		return 0
	}
	return shared
}

func usageWindowReset(window usage.Window, now time.Time) string {
	if window.ResetsAt.IsZero() {
		return "reset unavailable"
	}
	return formatUsageReset(window.ResetsAt, now)
}

// renderUsageWindow keeps the textual usage details intact and uses any
// remaining terminal width for a proportional bar. Narrow terminals retain
// the compact text-only representation.
func renderUsageWindow(width int, label string, used float64, reset string, barWidth int) string {
	prefix := fmt.Sprintf("  %-8s %6.1f%% used", label+":", used)
	if barWidth < usageProgressMinWidth {
		return prefix + "  " + mutedStyle.Render(reset)
	}
	return prefix + "  " + usageProgressBar(used, barWidth) + "  " + mutedStyle.Render(reset)
}

func usageProgressWidth(width int, label, reset string) int {
	prefix := fmt.Sprintf("  %-8s %6.1f%% used", label+":", 100.0)
	return width - lipgloss.Width(prefix) - lipgloss.Width(reset) - 4
}

func usageProgressBar(used float64, width int) string {
	width = max(usageProgressMinWidth, width)
	innerWidth := width - 2
	percent := min(100, max(0, int(used)))
	filled := int(float64(innerWidth)*float64(percent)/100.0 + 0.5)
	return "[" + failoverStyle.Render(strings.Repeat("█", filled)) + mutedStyle.Render(strings.Repeat("░", innerWidth-filled)) + "]"
}

func formatUsageReset(at, now time.Time) string {
	date := "resets " + at.Local().Format("2006-01-02 15:04")
	remaining := at.Sub(now)
	if remaining <= 0 {
		return date + " (overdue)"
	}
	minutes := int(remaining.Round(time.Minute).Minutes())
	days, minutes := minutes/(24*60), minutes%(24*60)
	hours, minutes := minutes/60, minutes%60
	parts := make([]string, 0, 3)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 || days > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	parts = append(parts, fmt.Sprintf("%dm", minutes))
	return date + " (in " + strings.Join(parts, " ") + ")"
}
