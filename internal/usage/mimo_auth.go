// MiMo console cookies are short-lived (the platform documents them as
// 24-hour cookies). The browser silently re-mints them with the longer-lived
// Xiaomi Account session, so the same exchange is replayed here when a saved
// userId/passToken seed is configured. See provider_balances.go for the
// display-only fetchers.
package usage

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	mimoPlatformOrigin = "https://platform.xiaomimimo.com"
	mimoAccountOrigin  = "https://account.xiaomi.com"
	mimoSafePath       = "/api/v1/userProfile"
	mimoUserAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/143.0.0.0 Safari/537.36"
)

var mimoRequiredPlatformCookies = []string{
	"api-platform_serviceToken",
	"api-platform_slh",
	"api-platform_ph",
	"userId",
}

// mimoAuthError marks a response that means the platform session is stale, as
// opposed to a transport or protocol problem. Only these errors trigger an
// automatic account-seed refresh.
type mimoAuthError struct{ reason string }

func (e mimoAuthError) Error() string { return e.reason }

func mimoHTTPClient(configured *http.Client) *http.Client {
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if configured == nil {
		return &http.Client{CheckRedirect: noRedirect}
	}
	// Refresh relies on reading the redirect Location header and on capturing
	// Set-Cookie from a disallowed follow-up, so redirects must stay off even
	// when the fetcher is constructed with a custom client.
	clone := *configured
	clone.CheckRedirect = noRedirect
	return &clone
}

func refreshMiMoSession(ctx context.Context, client *http.Client, platform, account map[string]string) (map[string]string, error) {
	loginURL, err := generateMiMoLoginURL(ctx, client, platform)
	if err != nil {
		return nil, err
	}
	auth, err := exchangeMiMoAccountSession(ctx, client, account, loginURL)
	if err != nil {
		return nil, err
	}
	// The account exchange can rotate the seed; keep the newest values in
	// memory so later refreshes in this process keep working.
	if value, ok := auth["userId"].(string); ok && value != "" {
		account["userId"] = value
	}
	if value, ok := auth["passToken"].(string); ok && value != "" {
		account["passToken"] = value
	}
	stsURL, err := signedMiMoSTSURL(auth)
	if err != nil {
		return nil, err
	}
	refreshed, err := visitMiMoSTS(ctx, client, stsURL)
	if err != nil {
		return nil, err
	}
	if err := verifyMiMoSession(ctx, client, refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

// generateMiMoLoginURL asks MiMo for the signed Xiaomi Account login URL. The
// request path is fixed to a read-only GET endpoint so the follow-up is safe.
func generateMiMoLoginURL(ctx context.Context, client *http.Client, platform map[string]string) (string, error) {
	endpoint := mimoPlatformOrigin + "/api/v1/genLoginUrl?currentPath=" + url.QueryEscape(mimoSafePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	setMiMoHeaders(req, platform, "application/json, text/plain, */*")
	resp, body, err := doMiMo(ctx, client, req)
	if err != nil {
		return "", err
	}
	location := resp.Header.Get("Location")
	if location == "" {
		var payload struct {
			LoginURL string `json:"loginUrl"`
		}
		if json.Unmarshal(body, &payload) == nil {
			location = payload.LoginURL
		}
	}
	if location == "" {
		return "", fmt.Errorf("MiMo did not return a login URL")
	}
	if err := validateMiMoLoginURL(location); err != nil {
		return "", err
	}
	return location, nil
}

func validateMiMoLoginURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("MiMo returned an invalid login URL")
	}
	if !miMoOrigin(parsed, mimoAccountOrigin) {
		return fmt.Errorf("MiMo returned an unsafe Xiaomi Account login URL")
	}
	if parsed.Path != "/pass/serviceLogin" {
		return fmt.Errorf("MiMo returned an unexpected Xiaomi Account endpoint")
	}
	query := parsed.Query()
	if query.Get("sid") != "api-platform" {
		return fmt.Errorf("MiMo returned an unexpected Xiaomi Account service ID")
	}
	callback, err := url.Parse(query.Get("callback"))
	if err != nil || !miMoOrigin(callback, mimoPlatformOrigin) || callback.Path != "/sts" {
		return fmt.Errorf("MiMo returned an unsafe authentication callback")
	}
	return nil
}

// exchangeMiMoAccountSession redeems the long-lived userId/passToken seed for
// a fresh signed STS URL.
func exchangeMiMoAccountSession(ctx context.Context, client *http.Client, account map[string]string, loginURL string) (map[string]any, error) {
	parsed, err := url.Parse(loginURL)
	if err != nil {
		return nil, fmt.Errorf("MiMo returned an invalid login URL")
	}
	query := parsed.Query()
	query.Set("_json", "true")
	parsed.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	setMiMoHeaders(req, account, "application/json, text/plain, */*")
	resp, body, err := doMiMo(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Xiaomi Account rejected the saved session (HTTP %d)", resp.StatusCode)
	}
	text := strings.TrimSpace(string(body))
	text = strings.TrimPrefix(text, "&&&START&&&")
	auth, err := parseMiMoAccountPayload(text)
	if err != nil {
		return nil, err
	}
	if auth == nil {
		return nil, fmt.Errorf("Xiaomi Account session expired; sign in at %s and update providers.mimo.account_cookie", mimoAccountOrigin)
	}
	return auth, nil
}

func parseMiMoAccountPayload(text string) (map[string]any, error) {
	root, err := decodeObject([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("Xiaomi Account requires interactive sign-in; sign in at %s and update providers.mimo.account_cookie", mimoAccountOrigin)
	}
	if code, ok := numberValue(root["code"]); !ok || code != 0 {
		return nil, nil
	}
	if location, _ := root["location"].(string); location == "" {
		return nil, nil
	}
	return root, nil
}

func signedMiMoSTSURL(auth map[string]any) (string, error) {
	location, _ := auth["location"].(string)
	parsed, err := url.Parse(location)
	if err != nil || !miMoOrigin(parsed, mimoPlatformOrigin) || parsed.Path != "/sts" {
		return "", fmt.Errorf("Xiaomi Account returned an unsafe MiMo STS URL")
	}
	query := parsed.Query()
	if query.Get("clientSign") != "" || query.Get("_ssign") != "" {
		return location, nil
	}
	nonce, ok := miMoNonce(auth["nonce"])
	ssecurity, _ := auth["ssecurity"].(string)
	if !ok || ssecurity == "" {
		return "", fmt.Errorf("Xiaomi Account response cannot be signed")
	}
	digest := sha1.Sum([]byte("nonce=" + nonce + "&" + ssecurity))
	query.Set("clientSign", base64.StdEncoding.EncodeToString(digest[:]))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func miMoNonce(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, value != ""
	case json.Number:
		return value.String(), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	default:
		return "", false
	}
}

// visitMiMoSTS follows the signed SSO callback. It must not follow redirects:
// the platform cookies arrive as Set-Cookie on the callback itself.
func visitMiMoSTS(ctx context.Context, client *http.Client, stsURL string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stsURL, nil)
	if err != nil {
		return nil, err
	}
	setMiMoHeaders(req, nil, "text/html,*/*")
	resp, _, err := doMiMo(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("MiMo STS returned HTTP %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "" {
		target, parseErr := url.Parse(location)
		if parseErr == nil {
			if base, baseErr := url.Parse(stsURL); baseErr == nil {
				target = base.ResolveReference(target)
			}
		}
		if parseErr != nil || !miMoOrigin(target, mimoPlatformOrigin) || target.Path != mimoSafePath {
			return nil, fmt.Errorf("MiMo STS returned an unsafe redirect")
		}
	}
	refreshed := make(map[string]string)
	for _, cookie := range resp.Cookies() {
		if cookie.Name != "" && cookie.Value != "" {
			refreshed[cookie.Name] = cookie.Value
		}
	}
	for _, name := range mimoRequiredPlatformCookies {
		if _, ok := cookieValue(refreshed, name); !ok {
			return nil, fmt.Errorf("MiMo STS did not issue the %s cookie", name)
		}
	}
	return refreshed, nil
}

func verifyMiMoSession(ctx context.Context, client *http.Client, cookies map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mimoPlatformOrigin+mimoSafePath, nil)
	if err != nil {
		return err
	}
	setMiMoHeaders(req, cookies, "application/json, text/plain, */*")
	resp, _, err := doMiMo(ctx, client, req)
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound:
		return fmt.Errorf("MiMo rejected the new platform session (HTTP %d)", resp.StatusCode)
	case resp.StatusCode >= 400:
		return fmt.Errorf("MiMo session verification returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func setMiMoHeaders(req *http.Request, cookies map[string]string, accept string) {
	if header := cookieHeader(cookies); header != "" {
		req.Header.Set("Cookie", header)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("x-timeZone", "UTC"+time.Now().Format("-07:00"))
	req.Header.Set("Origin", mimoPlatformOrigin)
	req.Header.Set("Referer", mimoPlatformOrigin+"/#/console/balance")
	req.Header.Set("User-Agent", mimoUserAgent)
}

func doMiMo(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("read MiMo response: %w", err)
	}
	return resp, body, nil
}

func miMoOrigin(parsed *url.URL, origin string) bool {
	if parsed == nil {
		return false
	}
	base, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" &&
		strings.EqualFold(parsed.Hostname(), base.Hostname()) &&
		(parsed.Port() == "" || parsed.Port() == "443") &&
		parsed.User == nil
}
