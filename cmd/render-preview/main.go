// Command render-preview prints the main TUI with synthetic data for screenshot generation.
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/a-bali/gomodeltui/internal/ui"
)

func main() {
	screen, width, height := "main", 154, 42
	if len(os.Args) > 1 {
		screen = os.Args[1]
	}
	if len(os.Args) > 2 {
		if value, err := strconv.Atoi(os.Args[2]); err == nil {
			width = value
		}
	}
	if len(os.Args) > 3 {
		if value, err := strconv.Atoi(os.Args[3]); err == nil {
			height = value
		}
	}
	fmt.Fprint(os.Stdout, ui.PreviewScreen(screen, width, height))
}
