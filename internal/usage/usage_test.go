package usage

import (
	"strings"
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

func TestParseOpenCodeUsageUsesAbsoluteResetAndDoesNotInventOne(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	snapshot, err := parseOpenCode([]byte(`{"usage":{"rolling":{"percent":12.5,"resetAt":"2026-09-19T14:00:00Z"},"weekly":{"percent":45}}}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Windows[0].ResetsAt.Equal(time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("absolute reset=%s", snapshot.Windows[0].ResetsAt)
	}
	if !snapshot.Windows[1].ResetsAt.IsZero() {
		t.Fatalf("missing reset was rendered as %s", snapshot.Windows[1].ResetsAt)
	}
}

func TestParseCommandCodeUsage(t *testing.T) {
	snapshot, err := parseCommandCode([]byte(`{"credits":{"monthlyCredits":12.5,"monthlyCreditsGranted":25},"windowLimits":{"fiveHour":{"used":3,"cap":12,"resetAt":"2026-09-20T12:00:00Z"},"weekly":{"used":1,"cap":4,"resetAt":"2026-09-21T12:00:00Z"}}}`), time.Now())
	if err != nil || snapshot.Windows[0].Used != 25 || snapshot.Credits != "monthly credits: 50.0% used  $12.50 / $25.00 remaining" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestParseCommandCodeUsageAcceptsNumericReset(t *testing.T) {
	snapshot, err := parseCommandCode([]byte(`{"credits":{"monthlyCredits":12.5},"windowLimits":{"fiveHour":{"used":3,"cap":12,"resetAt":1789905600000},"weekly":{"used":1,"cap":4,"resetAt":1789992000}}}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Windows[0].ResetsAt.Unix() != 1789905600 || snapshot.Windows[1].ResetsAt.Unix() != 1789992000 {
		t.Fatalf("unexpected reset times: %+v", snapshot.Windows)
	}
}

func TestParseCommandSubscriptionReadsBillingReset(t *testing.T) {
	snapshot, err := parseCommandSubscription([]byte(`{"success":true,"data":{"currentPeriodEnd":1789992000000}}`), time.Now())
	if err != nil || snapshot.CreditResetAt.Unix() != 1789992000 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestCommandCodeCookieHeaderAcceptsBareSessionToken(t *testing.T) {
	got := commandCodeCookieHeader("session-token")
	if !strings.Contains(got, "__Secure-commandcode_prod_.session_token=session-token") || !strings.Contains(got, "__Secure-better-auth.session_token=session-token") {
		t.Fatalf("header=%q", got)
	}
	if got := commandCodeCookieHeader("Cookie: other=value; session=value"); got != "other=value; session=value" {
		t.Fatalf("header=%q", got)
	}
}

func TestParseCodexUsage(t *testing.T) {
	snapshot, err := parseCodex([]byte(`{"rate_limit":{"primary_window":{"used_percent":20,"reset_at":60},"secondary_window":{"used_percent":30,"reset_at":120}}}`), time.Now())
	if err != nil || len(snapshot.Windows) != 2 || snapshot.Windows[1].Used != 30 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
