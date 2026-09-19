package main

import (
	"log"

	"github.com/balia/gomodeltui/internal/config"
	"github.com/balia/gomodeltui/internal/gomodel"
	"github.com/balia/gomodeltui/internal/ui"
	"github.com/balia/gomodeltui/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client, err := gomodel.NewClient(cfg.URL, cfg.Token, nil)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := tea.NewProgram(ui.NewModel(client, usage.Available()...), tea.WithAltScreen()).Run(); err != nil {
		log.Fatal(err)
	}
}
