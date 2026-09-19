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
	Provider string
	Source   string
	Windows  []Window
	Credits  string
	Updated  time.Time
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.commandcode.ai/internal/billing/credits", nil)
	if err != nil {
		return Snapshot{}, err
	}
	req.Header.Set("Cookie", f.Cookie)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://commandcode.ai")
	req.Header.Set("Referer", "https://commandcode.ai/")
	return fetchJSON(f.Client, req, f.Provider(), "session cookie", parseCommandCode)
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
	var body struct {
		Usage struct {
			Rolling struct {
				Percent    float64 `json:"percent"`
				ResetInSec int64   `json:"resetInSec"`
			} `json:"rolling"`
			Weekly struct {
				Percent    float64 `json:"percent"`
				ResetInSec int64   `json:"resetInSec"`
			} `json:"weekly"`
			Monthly struct {
				Percent    float64 `json:"percent"`
				ResetInSec int64   `json:"resetInSec"`
			} `json:"monthly"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Snapshot{}, err
	}
	windows := []Window{{"5h", body.Usage.Rolling.Percent, now.Add(time.Duration(body.Usage.Rolling.ResetInSec) * time.Second)}, {"weekly", body.Usage.Weekly.Percent, now.Add(time.Duration(body.Usage.Weekly.ResetInSec) * time.Second)}}
	if body.Usage.Monthly.Percent > 0 || body.Usage.Monthly.ResetInSec > 0 {
		windows = append(windows, Window{"monthly", body.Usage.Monthly.Percent, now.Add(time.Duration(body.Usage.Monthly.ResetInSec) * time.Second)})
	}
	return Snapshot{Windows: windows}, nil
}

func parseCommandCode(raw json.RawMessage, now time.Time) (Snapshot, error) {
	var body struct {
		Credits struct {
			Monthly float64 `json:"monthlyCredits"`
		} `json:"credits"`
		Limits struct {
			FiveHour struct {
				Used, Cap float64
				ResetAt   time.Time `json:"resetAt"`
			} `json:"fiveHour"`
			Weekly struct {
				Used, Cap float64
				ResetAt   time.Time `json:"resetAt"`
			} `json:"weekly"`
		} `json:"windowLimits"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Snapshot{}, err
	}
	window := func(label string, used, cap float64, reset time.Time) Window {
		if cap == 0 {
			return Window{Label: label}
		}
		return Window{Label: label, Used: used * 100 / cap, ResetsAt: reset}
	}
	return Snapshot{Windows: []Window{window("5h", body.Limits.FiveHour.Used, body.Limits.FiveHour.Cap, body.Limits.FiveHour.ResetAt), window("weekly", body.Limits.Weekly.Used, body.Limits.Weekly.Cap, body.Limits.Weekly.ResetAt)}, Credits: fmt.Sprintf("$%.2f monthly credits remaining", body.Credits.Monthly)}, nil
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
