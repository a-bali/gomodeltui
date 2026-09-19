package usage

import (
	"testing"
	"time"
)

func TestParseOpenCodeUsage(t *testing.T) {
	snapshot, err := parseOpenCode([]byte(`{"usage":{"rolling":{"percent":12.5,"resetInSec":60},"weekly":{"percent":45,"resetInSec":120}}}`), time.Unix(0, 0))
	if err != nil || len(snapshot.Windows) != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.Windows[0].Used != 12.5 || !snapshot.Windows[0].ResetsAt.Equal(time.Unix(60, 0)) {
		t.Fatalf("unexpected rolling window: %+v", snapshot.Windows[0])
	}
}

func TestParseCommandCodeUsage(t *testing.T) {
	snapshot, err := parseCommandCode([]byte(`{"credits":{"monthlyCredits":12.5},"windowLimits":{"fiveHour":{"used":3,"cap":12,"resetAt":"2026-09-20T12:00:00Z"},"weekly":{"used":1,"cap":4,"resetAt":"2026-09-21T12:00:00Z"}}}`), time.Now())
	if err != nil || snapshot.Windows[0].Used != 25 || snapshot.Credits != "$12.50 monthly credits remaining" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestParseCodexUsage(t *testing.T) {
	snapshot, err := parseCodex([]byte(`{"rate_limit":{"primary_window":{"used_percent":20,"reset_at":60},"secondary_window":{"used_percent":30,"reset_at":120}}}`), time.Now())
	if err != nil || len(snapshot.Windows) != 2 || snapshot.Windows[1].Used != 30 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
