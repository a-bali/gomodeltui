package main

import (
	"log"
	"os"

	"github.com/balia/gomodeltui/internal/config"
	"github.com/balia/gomodeltui/internal/gomodel"
	"github.com/balia/gomodeltui/internal/ui"
	"github.com/balia/gomodeltui/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	client, err := gomodel.NewClient(cfg.GoModel.URL, cfg.GoModel.APIKey, nil)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := tea.NewProgram(ui.NewModel(client, usage.Available(cfg.Providers)...), tea.WithAltScreen()).Run(); err != nil {
		log.Fatal(err)
	}
}
