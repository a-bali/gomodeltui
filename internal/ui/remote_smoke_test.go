//go:build integration

package ui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/a-bali/gomodeltui/internal/chart"
	"github.com/a-bali/gomodeltui/internal/gomodel"
)

func TestRemoteLiveChartSmoke(t *testing.T) {
	client, err := gomodel.NewClient(os.Getenv("GOMODEL_URL"), os.Getenv("GOMODEL_API_KEY"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, err := client.LiveLogs(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	model := NewModel(nil)
	reader := bufio.NewReader(response.Body)
	terminal := 0
	for ctx.Err() == nil {
		event, readErr := gomodel.ReadEvent(reader)
		if readErr != nil {
			break
		}
		before := len(model.logs)
		_, _ = model.Update(eventMsg{event: event})
		if len(model.logs) > before && model.logs[len(model.logs)-1].Terminal {
			terminal++
		}
		if terminal >= 1 {
			break
		}
	}
	buckets := model.store.Snapshot(time.Now(), chart.Window1h)
	if terminal == 0 {
		t.Fatal("no terminal request observed in 30 seconds")
	}
	var total int
	for _, bucket := range buckets {
		total += bucket.Total()
	}
	fmt.Printf("remote smoke: terminal=%d chart_total=%d\n", terminal, total)
	if total == 0 {
		t.Fatal("terminal event did not produce chart data")
	}
}
