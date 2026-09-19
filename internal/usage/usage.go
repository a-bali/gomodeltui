// Package usage reads quota information from providers without changing account state.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Window struct {
	Label    string
	Used     float64
	ResetsAt time.Time
}

type Snapshot struct {
	Provider      string
	Source        string
	Windows       []Window
	Credits       string
	CreditResetAt time.Time
	Updated       time.Time
}

type Fetcher interface {
	Provider() string
	Fetch(context.Context) (Snapshot, error)
}

func Available() []Fetcher {
	var fetchers []Fetcher
	if key := strings.TrimSpace(os.Getenv("OPENCODE_API_KEY")); key != "" {
		fetchers = append(fetchers, OpenCodeGo{APIKey: key, Client: http.DefaultClient})
	}
	if cookie := strings.TrimSpace(os.Getenv("COMMANDCODE_COOKIE")); cookie != "" {
		fetchers = append(fetchers, CommandCode{Cookie: cookie, Client: http.DefaultClient})
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".codex", "auth.json")
		if _, err := os.Stat(path); err == nil {
			fetchers = append(fetchers, Codex{AuthPath: path, Client: http.DefaultClient})
		}
	}
	return fetchers
}

type OpenCodeGo struct {
	APIKey string
	Client *http.Client
}

func (OpenCodeGo) Provider() string { return "OpenCode Go" }

func (f OpenCodeGo) Fetch(ctx context.Context) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://opencode.ai/zen/go/v1/usage", nil)
	if err != nil {
		return Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+f.APIKey)
	return fetchJSON(f.Client, req, f.Provider(), "API key", parseOpenCode)
}

type CommandCode struct {
	Cookie string
	Client *http.Client
}

func (CommandCode) Provider() string { return "Command Code" }

func (f CommandCode) Fetch(ctx context.Context) (Snapshot, error) {
	creditsRequest, err := f.request(ctx, "/internal/billing/credits")
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := fetchJSON(f.Client, creditsRequest, f.Provider(), "session cookie", parseCommandCode)
	if err != nil {
		return Snapshot{}, err
	}
	// Subscription data is enrichment only: credits and rolling windows remain
	// useful when this endpoint is temporarily unavailable.
	subscriptionRequest, err := f.request(ctx, "/internal/billing/subscriptions")
	if err == nil {
		if subscription, subscriptionErr := fetchJSON(f.Client, subscriptionRequest, f.Provider(), "session cookie", parseCommandSubscription); subscriptionErr == nil {
			snapshot.CreditResetAt = subscription.CreditResetAt
		}
	}
	return snapshot, nil
}

func (f CommandCode) request(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.commandcode.ai"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", commandCodeCookieHeader(f.Cookie))
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://commandcode.ai")
	req.Header.Set("Referer", "https://commandcode.ai/")
	return req, nil
}

func commandCodeCookieHeader(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSpace(strings.TrimPrefix(value, "Cookie:"))
	if !strings.Contains(value, "=") && !strings.Contains(value, ";") {
		// A bare token does not identify which Better Auth deployment issued it.
		// Send it under every session name Command Code has used in production;
		// the server reads only its current cookie name.
		names := []string{
			"__Secure-commandcode_prod_.session_token",
			"commandcode_prod_.session_token",
			"__Host-commandcode_prod_.session_token",
			"__Host-better-auth.session_token",
			"__Secure-better-auth.session_token",
			"better-auth.session_token",
		}
		cookies := make([]string, len(names))
		for index, name := range names {
			cookies[index] = name + "=" + value
		}
		return strings.Join(cookies, "; ")
	}
	return value
}

type Codex struct {
	AuthPath string
	Client   *http.Client
}

func (Codex) Provider() string { return "ChatGPT / Codex" }

func (f Codex) Fetch(ctx context.Context) (Snapshot, error) {
	data, err := os.ReadFile(f.AuthPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read Codex credentials: %w", err)
	}
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return Snapshot{}, fmt.Errorf("parse Codex credentials: %w", err)
	}
	if auth.Tokens.AccessToken == "" {
		return Snapshot{}, fmt.Errorf("Codex credentials have no access token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	return fetchJSON(f.Client, req, f.Provider(), "local Codex sign-in", parseCodex)
}

func fetchJSON(client *http.Client, req *http.Request, provider, source string, parse func(json.RawMessage, time.Time) (Snapshot, error)) (Snapshot, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Snapshot{}, fmt.Errorf("%s usage returned HTTP %d", provider, resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Snapshot{}, fmt.Errorf("decode %s usage: %w", provider, err)
	}
	snapshot, err := parse(raw, time.Now())
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Provider, snapshot.Source, snapshot.Updated = provider, source, time.Now()
	return snapshot, nil
}

func parseOpenCode(raw json.RawMessage, now time.Time) (Snapshot, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return Snapshot{}, err
	}
	usageData, ok := body["usage"].(map[string]any)
	if !ok {
		return Snapshot{}, fmt.Errorf("OpenCode Go usage response has no usage object")
	}
	rolling, ok := openCodeWindow(usageData["rolling"], "5h", now)
	if !ok {
		return Snapshot{}, fmt.Errorf("OpenCode Go usage response has no rolling window")
	}
	windows := []Window{rolling}
	for _, item := range []struct{ key, label string }{{"weekly", "weekly"}, {"monthly", "monthly"}} {
		if window, ok := openCodeWindow(usageData[item.key], item.label, now); ok {
			windows = append(windows, window)
		}
	}
	return Snapshot{Windows: windows}, nil
}

func openCodeWindow(value any, label string, now time.Time) (Window, bool) {
	data, ok := value.(map[string]any)
	if !ok {
		return Window{}, false
	}
	percent, ok := numberValue(data["percent"])
	if !ok {
		return Window{}, false
	}
	window := Window{Label: label, Used: percent}
	for _, key := range []string{"resetInSec", "resetInSeconds", "resetSeconds", "reset_sec", "resetsInSec"} {
		if seconds, ok := numberValue(data[key]); ok && seconds > 0 {
			window.ResetsAt = now.Add(time.Duration(seconds * float64(time.Second)))
			return window, true
		}
	}
	for _, key := range []string{"resetAt", "resetsAt", "reset_at", "resets_at", "nextReset", "renewAt"} {
		if resetAt, ok := dateValue(data[key]); ok {
			window.ResetsAt = resetAt
			break
		}
	}
	return window, true
}

func numberValue(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		var parsed float64
		_, err := fmt.Sscan(value, &parsed)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func dateValue(value any) (time.Time, bool) {
	if text, ok := value.(string); ok {
		parsed, err := time.Parse(time.RFC3339, text)
		return parsed, err == nil
	}
	seconds, ok := numberValue(value)
	if !ok || seconds <= 0 {
		return time.Time{}, false
	}
	if seconds > 1e11 { // Unix milliseconds.
		seconds /= 1000
	}
	return time.Unix(int64(seconds), 0), true
}

func parseCommandCode(raw json.RawMessage, _ time.Time) (Snapshot, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return Snapshot{}, err
	}
	credits, _ := body["credits"].(map[string]any)
	limits, _ := body["windowLimits"].(map[string]any)
	window := func(label string, value any) Window {
		limit, _ := value.(map[string]any)
		used, _ := numberValue(limit["used"])
		cap, _ := numberValue(limit["cap"])
		reset, _ := dateValue(limit["resetAt"])
		if cap == 0 {
			return Window{Label: label}
		}
		return Window{Label: label, Used: used * 100 / cap, ResetsAt: reset}
	}
	monthly, _ := numberValue(credits["monthlyCredits"])
	return Snapshot{Windows: []Window{window("5h", limits["fiveHour"]), window("weekly", limits["weekly"])}, Credits: fmt.Sprintf("$%.2f monthly credits remaining", monthly)}, nil
}

func parseCommandSubscription(raw json.RawMessage, _ time.Time) (Snapshot, error) {
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			CurrentPeriodEnd any `json:"currentPeriodEnd"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Snapshot{}, err
	}
	if !body.Success {
		return Snapshot{}, fmt.Errorf("Command Code subscription lookup was unsuccessful")
	}
	reset, _ := dateValue(body.Data.CurrentPeriodEnd)
	return Snapshot{CreditResetAt: reset}, nil
}

func parseCodex(raw json.RawMessage, now time.Time) (Snapshot, error) {
	var body struct {
		RateLimit struct {
			Primary struct {
				UsedPercent float64 `json:"used_percent"`
				ResetAt     int64   `json:"reset_at"`
			} `json:"primary_window"`
			Secondary struct {
				UsedPercent float64 `json:"used_percent"`
				ResetAt     int64   `json:"reset_at"`
			} `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Snapshot{}, err
	}
	toTime := func(seconds int64) time.Time {
		if seconds == 0 {
			return time.Time{}
		}
		return time.Unix(seconds, 0)
	}
	return Snapshot{Windows: []Window{{"5h", body.RateLimit.Primary.UsedPercent, toTime(body.RateLimit.Primary.ResetAt)}, {"weekly", body.RateLimit.Secondary.UsedPercent, toTime(body.RateLimit.Secondary.ResetAt)}}}, nil
}
