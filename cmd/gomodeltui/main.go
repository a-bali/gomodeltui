package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/a-bali/gomodeltui/internal/config"
	"github.com/a-bali/gomodeltui/internal/gomodel"
	"github.com/a-bali/gomodeltui/internal/ui"
	"github.com/a-bali/gomodeltui/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
)

// Build metadata is overridden at release time with -ldflags, for example:
//
//	-X main.version=1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-v", "-version", "--version":
			fmt.Printf("gomodeltui %s (commit %s, built %s)\n", version, commit, date)
			return
		}
	}
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print(config.Help())
			return
		}
		log.Fatal(err)
	}
	client, err := gomodel.NewClient(cfg.GoModel.URL, cfg.GoModel.APIKey, nil)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := tea.NewProgram(ui.NewModelWithRetention(client, cfg.Retention(), usage.Available(cfg.Providers)...), tea.WithAltScreen()).Run(); err != nil {
		log.Fatal(err)
	}
}
