package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type OpenRouter struct {
	APIKey string
	Client *http.Client
}

func (OpenRouter) Provider() string { return "OpenRouter" }

func (f OpenRouter) Fetch(ctx context.Context) (Snapshot, error) {
	base := "https://openrouter.ai/api/v1"
	type result struct {
		endpoint string
		body     []byte
		err      error
	}
	results := make(chan result, 2)
	for _, endpoint := range []string{"key", "credits"} {
		endpoint := endpoint
		go func() {
			body, err := getProviderJSON(ctx, f.Client, base+"/"+endpoint, f.APIKey, nil)
			results <- result{endpoint, body, err}
		}()
	}
	responses := make(map[string]result, 2)
	for range 2 {
		select {
		case item := <-results:
			responses[item.endpoint] = item
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		}
	}
	keyData, keyErr := responses["key"].body, responses["key"].err
	creditsData, creditsErr := responses["credits"].body, responses["credits"].err
	snapshot := Snapshot{Provider: f.Provider(), Source: "API key", Updated: time.Now()}
	var failures []string
	keyAvailable := false
	if keyErr == nil {
		if err := parseOpenRouterKey(keyData, &snapshot); err != nil {
			failures = append(failures, "key response: "+err.Error())
		} else {
			keyAvailable = true
		}
	} else {
		failures = append(failures, "key endpoint: "+keyErr.Error())
	}
	if creditsErr == nil {
		if err := parseOpenRouterCredits(creditsData, &snapshot); err != nil {
			snapshot.Details = append(snapshot.Details, "Credit balance unavailable: "+err.Error())
		}
	} else {
		snapshot.Details = append(snapshot.Details, "Credit balance unavailable: "+creditsErr.Error())
	}
	if !keyAvailable && snapshot.Credits == "" {
		if len(failures) == 0 {
			failures = append(failures, "no key quota data was returned")
		}
		return Snapshot{}, fmt.Errorf("OpenRouter usage unavailable (%s)", strings.Join(failures, "; "))
	}
	return snapshot, nil
}

func parseOpenRouterKey(raw []byte, snapshot *Snapshot) error {
	root, err := decodeObject(raw)
	if err != nil {
		return err
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("response has no data object")
	}
	limit, hasLimit, err := optionalNumber(data, "limit")
	if err != nil {
		return err
	}
	usageValue, hasUsage, err := optionalNumber(data, "usage")
	if err != nil {
		return err
	}
	if !hasLimit || limit <= 0 {
		snapshot.Details = append(snapshot.Details, "API key limit: none configured")
	} else {
		remaining, hasRemaining, err := optionalNumber(data, "limit_remaining")
		if err != nil {
			return err
		}
		used := usageValue
		usedKnown := hasUsage
		if hasRemaining {
			used = limit - remaining
			usedKnown = true
		} else {
			reset, _ := data["limit_reset"].(string)
			field := map[string]string{"daily": "usage_daily", "weekly": "usage_weekly", "monthly": "usage_monthly"}[strings.ToLower(reset)]
			if field != "" {
				if windowUsage, ok, parseErr := optionalNumber(data, field); parseErr != nil {
					return parseErr
				} else if ok {
					used = windowUsage
					usedKnown = true
				}
			}
		}
		if usedKnown {
			used = math.Max(0, math.Min(limit, used))
			label := "key"
			if reset, ok := data["limit_reset"].(string); ok && strings.TrimSpace(reset) != "" {
				label += " " + strings.TrimSpace(reset)
			}
			snapshot.Windows = append(snapshot.Windows, Window{Label: label, Used: used * 100 / limit})
			snapshot.Details = append(snapshot.Details, fmt.Sprintf("API key cap: $%.2f · $%.2f remaining", limit, math.Max(0, limit-used)))
		} else {
			snapshot.Details = append(snapshot.Details, fmt.Sprintf("API key cap: $%.2f", limit))
		}
	}
	if hasUsage {
		snapshot.Details = append(snapshot.Details, fmt.Sprintf("Lifetime key spend: $%.2f", usageValue))
	}
	for _, period := range []struct{ label, field string }{
		{"Today", "usage_daily"}, {"This week", "usage_weekly"}, {"This month", "usage_monthly"},
	} {
		value, ok, err := optionalNumber(data, period.field)
		if err != nil {
			return err
		}
		if ok {
			snapshot.Details = append(snapshot.Details, fmt.Sprintf("%s key spend: $%.2f", period.label, value))
		}
	}
	return nil
}

func parseOpenRouterCredits(raw []byte, snapshot *Snapshot) error {
	root, err := decodeObject(raw)
	if err != nil {
		return err
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("response has no data object")
	}
	total, ok, err := optionalNumber(data, "total_credits")
	if err != nil || !ok {
		return fmt.Errorf("missing or invalid total_credits")
	}
	used, ok, err := optionalNumber(data, "total_usage")
	if err != nil || !ok {
		return fmt.Errorf("missing or invalid total_usage")
	}
	remaining := math.Max(0, total-used)
	snapshot.Credits = fmt.Sprintf("Credits: $%.2f remaining ($%.2f used of $%.2f)", remaining, used, total)
	return nil
}

type DeepSeek struct {
	APIKey string
	Client *http.Client
}

func (DeepSeek) Provider() string { return "DeepSeek" }

func (f DeepSeek) Fetch(ctx context.Context) (Snapshot, error) {
	raw, err := getProviderJSON(ctx, f.Client, "https://api.deepseek.com/user/balance", f.APIKey, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("DeepSeek usage: %w", err)
	}
	snapshot, err := parseDeepSeek(raw)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Provider, snapshot.Source, snapshot.Updated = f.Provider(), "API key", time.Now()
	return snapshot, nil
}

func parseDeepSeek(raw []byte) (Snapshot, error) {
	root, err := decodeObject(raw)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode DeepSeek balance: %w", err)
	}
	infos, ok := root["balance_infos"].([]any)
	if !ok || len(infos) == 0 {
		return Snapshot{}, fmt.Errorf("DeepSeek balance response has no balance_infos")
	}
	type balance struct {
		currency             string
		total, paid, granted float64
	}
	balances := make([]balance, 0, len(infos))
	for _, item := range infos {
		info, ok := item.(map[string]any)
		if !ok {
			continue
		}
		currency, _ := info["currency"].(string)
		total, err1 := requiredNumber(info, "total_balance")
		paid, err2 := requiredNumber(info, "topped_up_balance")
		granted, err3 := requiredNumber(info, "granted_balance")
		if currency != "" && err1 == nil && err2 == nil && err3 == nil {
			balances = append(balances, balance{currency, total, paid, granted})
		}
	}
	if len(balances) == 0 {
		return Snapshot{}, fmt.Errorf("DeepSeek balance response has no valid currency balances")
	}
	selected := balances[0]
	for _, item := range balances {
		if strings.EqualFold(item.currency, "USD") {
			selected = item
			break
		}
	}
	symbol := currencySymbol(selected.currency)
	snapshot := Snapshot{Credits: fmt.Sprintf("Balance: %s%.2f", symbol, selected.total)}
	snapshot.Details = append(snapshot.Details,
		fmt.Sprintf("Paid: %s%.2f · Granted: %s%.2f", symbol, selected.paid, symbol, selected.granted))
	if available, ok := root["is_available"].(bool); ok && !available {
		snapshot.Details = append(snapshot.Details, "Balance unavailable for API calls")
	}
	if len(balances) > 1 && !strings.EqualFold(selected.currency, balances[0].currency) {
		snapshot.Details = append(snapshot.Details, fmt.Sprintf("Other balance: %s%.2f (%s)", currencySymbol(balances[0].currency), balances[0].total, balances[0].currency))
	}
	return snapshot, nil
}

type MiMo struct {
	Cookie        string
	AccountCookie string
	Client        *http.Client

	mu       sync.Mutex
	platform map[string]string
	account  map[string]string
}

func (*MiMo) Provider() string { return "Xiaomi MiMo" }

func (f *MiMo) Fetch(ctx context.Context) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	client := mimoHTTPClient(f.Client)
	platform := f.platformCookies()
	account := f.accountCookies()

	var (
		data       map[string][]byte
		balanceErr error
		authFailed bool
		err        error
	)
	if validMiMoCookies(platform) {
		data, balanceErr, authFailed, err = f.fetchEndpoints(ctx, client, platform)
		if err != nil {
			return Snapshot{}, err
		}
	}
	if authFailed || !validMiMoCookies(platform) {
		if !hasMiMoAccountSeed(account) {
			if !validMiMoCookies(platform) {
				return Snapshot{}, fmt.Errorf("Xiaomi MiMo requires api-platform_serviceToken and userId cookies")
			}
			return Snapshot{}, fmt.Errorf("Xiaomi MiMo session expired; sign in at %s/#/console/balance and update providers.mimo.cookie, or set providers.mimo.account_cookie for automatic refresh", mimoPlatformOrigin)
		}
		refreshed, refreshErr := refreshMiMoSession(ctx, client, platform, account)
		if refreshErr != nil {
			return Snapshot{}, fmt.Errorf("Xiaomi MiMo session expired and automatic refresh failed (%v); sign in at %s/#/console/balance and update providers.mimo.account_cookie", refreshErr, mimoPlatformOrigin)
		}
		f.platform, f.account = refreshed, account
		data, balanceErr, authFailed, err = f.fetchEndpoints(ctx, client, refreshed)
		if err != nil {
			return Snapshot{}, err
		}
		if authFailed {
			return Snapshot{}, fmt.Errorf("Xiaomi MiMo rejected the automatically refreshed session; sign in at %s/#/console/balance and update providers.mimo.account_cookie", mimoPlatformOrigin)
		}
	}
	if balanceErr != nil {
		return Snapshot{}, fmt.Errorf("Xiaomi MiMo balance: %w", balanceErr)
	}
	snapshot, err := parseMiMo(data["balance"], data["tokenPlan/detail"], data["tokenPlan/usage"])
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Provider, snapshot.Source, snapshot.Updated = f.Provider(), "session cookie", time.Now()
	if len(f.platform) > 0 {
		snapshot.Source = "session cookie (auto-refreshed)"
	}
	return snapshot, nil
}

func (f *MiMo) platformCookies() map[string]string {
	if len(f.platform) > 0 {
		return f.platform
	}
	return parseMiMoCookie(f.Cookie)
}

func (f *MiMo) accountCookies() map[string]string {
	if len(f.account) > 0 {
		return f.account
	}
	return parseMiMoCookie(f.AccountCookie)
}

func (f *MiMo) fetchEndpoints(ctx context.Context, client *http.Client, cookies map[string]string) (map[string][]byte, error, bool, error) {
	base := "https://platform.xiaomimimo.com/api/v1/"
	type result struct {
		endpoint string
		body     []byte
		err      error
	}
	results := make(chan result, 3)
	for _, endpoint := range []string{"balance", "tokenPlan/detail", "tokenPlan/usage"} {
		endpoint := endpoint
		go func() {
			body, err := getMiMoJSON(ctx, client, base+endpoint, cookieHeader(cookies))
			results <- result{endpoint, body, err}
		}()
	}
	data := make(map[string][]byte, 3)
	var balanceErr error
	authFailed := false
	for range 3 {
		select {
		case item := <-results:
			if item.err == nil {
				data[item.endpoint] = item.body
				continue
			}
			var authErr mimoAuthError
			if errors.As(item.err, &authErr) {
				authFailed = true
			}
			if item.endpoint == "balance" {
				balanceErr = item.err
			}
		case <-ctx.Done():
			return nil, nil, false, fmt.Errorf("Xiaomi MiMo usage: %w", ctx.Err())
		}
	}
	return data, balanceErr, authFailed, nil
}

func getMiMoJSON(ctx context.Context, client *http.Client, endpoint, cookie string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("x-timeZone", "UTC"+time.Now().Format("-07:00"))
	req.Header.Set("Origin", "https://platform.xiaomimimo.com")
	req.Header.Set("Referer", "https://platform.xiaomimimo.com/#/console/balance")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/143.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read Xiaomi MiMo response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound {
		return nil, mimoAuthError{fmt.Sprintf("Xiaomi MiMo returned HTTP %d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if root, decodeErr := decodeObject(body); decodeErr == nil {
		if code, ok := numberValue(root["code"]); ok && code == 401 {
			return nil, mimoAuthError{"Xiaomi MiMo rejected the session cookie"}
		}
	}
	return body, nil
}

func parseMiMo(balanceRaw, detailRaw, usageRaw []byte) (Snapshot, error) {
	root, err := decodeObject(balanceRaw)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode Xiaomi MiMo balance: %w", err)
	}
	code, ok := numberValue(root["code"])
	if !ok || code != 0 {
		return Snapshot{}, fmt.Errorf("Xiaomi MiMo balance response was unsuccessful (code %v)", root["code"])
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		return Snapshot{}, fmt.Errorf("Xiaomi MiMo balance response has no data object")
	}
	balance, err := requiredNumber(data, "balance")
	if err != nil {
		return Snapshot{}, fmt.Errorf("invalid Xiaomi MiMo balance")
	}
	currency, _ := data["currency"].(string)
	if currency == "" {
		return Snapshot{}, fmt.Errorf("Xiaomi MiMo balance response has no currency")
	}
	symbol := currencySymbol(currency)
	snapshot := Snapshot{Credits: fmt.Sprintf("Balance: %s%.2f", symbol, balance)}
	if cash, ok, _ := optionalNumber(data, "cashBalance"); ok {
		detail := fmt.Sprintf("Paid: %s%.2f", symbol, cash)
		if gift, hasGift, _ := optionalNumber(data, "giftBalance"); hasGift {
			detail += fmt.Sprintf(" · Granted: %s%.2f", symbol, gift)
		}
		snapshot.Details = append(snapshot.Details, detail)
	}
	planName, periodEnd := parseMiMoDetail(detailRaw)
	if len(usageRaw) > 0 {
		if used, limit, percent, ok := parseMiMoPlanUsage(usageRaw); ok {
			label := "monthly"
			if planName != "" {
				label = "monthly " + planName
			}
			snapshot.Windows = append(snapshot.Windows, Window{Label: label, Used: percent, ResetsAt: periodEnd})
			if limit > 0 {
				snapshot.Details = append(snapshot.Details, fmt.Sprintf("Token plan: %s / %s tokens", formatInteger(used), formatInteger(limit)))
			}
		}
	}
	if planName != "" && len(snapshot.Windows) == 0 {
		snapshot.Details = append(snapshot.Details, "Token plan: "+planName)
	}
	return snapshot, nil
}

func parseMiMoDetail(raw []byte) (string, time.Time) {
	root, err := decodeObject(raw)
	if err != nil {
		return "", time.Time{}
	}
	if code, ok := numberValue(root["code"]); !ok || code != 0 {
		return "", time.Time{}
	}
	data, _ := root["data"].(map[string]any)
	name, _ := data["planCode"].(string)
	period := time.Time{}
	if value, ok := data["currentPeriodEnd"].(string); ok {
		period, _ = time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	}
	return name, period
}

func parseMiMoPlanUsage(raw []byte) (used, limit int64, percent float64, ok bool) {
	root, err := decodeObject(raw)
	if err != nil {
		return 0, 0, 0, false
	}
	code, valid := numberValue(root["code"])
	if !valid || code != 0 {
		return 0, 0, 0, false
	}
	data, _ := root["data"].(map[string]any)
	month, _ := data["monthUsage"].(map[string]any)
	items, _ := month["items"].([]any)
	if len(items) == 0 {
		return 0, 0, 0, false
	}
	item, _ := items[0].(map[string]any)
	usedValue, err1 := requiredNumber(item, "used")
	limitValue, err2 := requiredNumber(item, "limit")
	if err1 != nil || err2 != nil || usedValue < 0 || limitValue <= 0 || usedValue > math.MaxInt64 || limitValue > math.MaxInt64 {
		return 0, 0, 0, false
	}
	used, limit = int64(usedValue), int64(limitValue)
	if value, valid := numberValue(item["percent"]); valid {
		percent = value
	} else {
		percent = usedValue * 100 / limitValue
	}
	return used, limit, math.Max(0, math.Min(100, percent)), true
}

func normalizeMiMoCookie(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "cookie:") {
		value = strings.TrimSpace(value[len("cookie:"):])
	}
	return value
}

func parseMiMoCookie(value string) map[string]string {
	cookies := make(map[string]string)
	for _, part := range strings.Split(normalizeMiMoCookie(value), ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if key != "" {
			cookies[key] = val
		}
	}
	return cookies
}

func cookieHeader(cookies map[string]string) string {
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookies[name])
	}
	return strings.Join(parts, "; ")
}

func cookieValue(cookies map[string]string, name string) (string, bool) {
	for key, value := range cookies {
		if strings.EqualFold(key, name) && strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	return "", false
}

func validMiMoCookies(cookies map[string]string) bool {
	_, serviceToken := cookieValue(cookies, "api-platform_serviceToken")
	_, userID := cookieValue(cookies, "userId")
	return serviceToken && userID
}

func hasMiMoAccountSeed(cookies map[string]string) bool {
	_, userID := cookieValue(cookies, "userId")
	_, passToken := cookieValue(cookies, "passToken")
	return userID && passToken
}

func getProviderJSON(ctx context.Context, configured *http.Client, endpoint, token string, extra http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	for key, values := range extra {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	client := configured
	if client == nil {
		client = http.DefaultClient
	}
	return providerResponse(ctx, client, req, "provider")
}

func providerResponse(ctx context.Context, client *http.Client, req *http.Request, provider string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", provider, err)
	}
	return body, nil
}

func decodeObject(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("response is not a JSON object")
	}
	return value, nil
}

func optionalNumber(object map[string]any, key string) (float64, bool, error) {
	value, exists := object[key]
	if !exists || value == nil {
		return 0, false, nil
	}
	parsed, ok := numberValue(value)
	if !ok || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false, fmt.Errorf("%s is not a finite number", key)
	}
	return parsed, true, nil
}

func requiredNumber(object map[string]any, key string) (float64, error) {
	value, ok, err := optionalNumber(object, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("%s is missing", key)
	}
	return value, nil
}

func currencySymbol(currency string) string {
	if strings.EqualFold(currency, "CNY") {
		return "¥"
	}
	return "$"
}

func formatInteger(value int64) string {
	text := fmt.Sprintf("%d", value)
	if len(text) <= 3 {
		return text
	}
	first := len(text) % 3
	if first == 0 {
		first = 3
	}
	var grouped strings.Builder
	grouped.WriteString(text[:first])
	for index := first; index < len(text); index += 3 {
		grouped.WriteByte(',')
		grouped.WriteString(text[index : index+3])
	}
	return grouped.String()
}
