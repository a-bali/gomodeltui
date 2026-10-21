package usage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func testResponse(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}

const testMiMoLoginURL = "https://account.xiaomi.com/pass/serviceLogin?sid=api-platform&callback=https%3A%2F%2Fplatform.xiaomimimo.com%2Fsts"

func TestParseMiMoCookieHeader(t *testing.T) {
	cookies := parseMiMoCookie("Cookie: api-platform_serviceToken=token=with=equals; userId=42; api-platform_slh=s; api-platform_ph=p")
	if len(cookies) != 4 {
		t.Fatalf("cookies=%v", cookies)
	}
	if value, ok := cookieValue(cookies, "API-PLATFORM_SERVICETOKEN"); !ok || value != "token=with=equals" {
		t.Fatalf("service token=%q ok=%v", value, ok)
	}
	if !validMiMoCookies(cookies) {
		t.Fatal("expected cookies to be valid")
	}
	header := cookieHeader(cookies)
	if !strings.Contains(header, "api-platform_serviceToken=token=with=equals") {
		t.Fatalf("header=%q", header)
	}
}

func TestSignedMiMoSTSURL(t *testing.T) {
	auth := map[string]any{
		"location":  "https://platform.xiaomimimo.com/sts?nonce=123&ssecurity=sec",
		"nonce":     "123",
		"ssecurity": "sec",
	}
	got, err := signedMiMoSTSURL(auth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "clientSign=BAIxzFdclO9Lfvgul2UbTLVvV5Q%3D") {
		t.Fatalf("signed url=%q", got)
	}
}

func TestValidMiMoLoginURLRejectsUntrustedHosts(t *testing.T) {
	if err := validateMiMoLoginURL(testMiMoLoginURL); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"https://evil.example/pass/serviceLogin?sid=api-platform&callback=https%3A%2F%2Fplatform.xiaomimimo.com%2Fsts",
		"http://account.xiaomi.com/pass/serviceLogin?sid=api-platform&callback=https%3A%2F%2Fplatform.xiaomimimo.com%2Fsts",
		"https://account.xiaomi.com/pass/serviceLogin?sid=other&callback=https%3A%2F%2Fplatform.xiaomimimo.com%2Fsts",
		"https://account.xiaomi.com/pass/serviceLogin?sid=api-platform&callback=https%3A%2F%2Fevil.example%2Fsts",
	} {
		if err := validateMiMoLoginURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestMiMoFetchRefreshesExpiredSession(t *testing.T) {
	var mu sync.Mutex
	var seenCookies []string
	record := func(req *http.Request) {
		mu.Lock()
		seenCookies = append(seenCookies, req.Header.Get("Cookie"))
		mu.Unlock()
	}
	refreshed := false
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		record(req)
		switch {
		case req.URL.Host == "platform.xiaomimimo.com" && req.URL.Path == "/api/v1/genLoginUrl":
			headers := http.Header{}
			headers.Set("Location", testMiMoLoginURL)
			return testResponse(http.StatusFound, "", headers), nil
		case req.URL.Host == "account.xiaomi.com" && req.URL.Path == "/pass/serviceLogin":
			return testResponse(http.StatusOK, `&&&START&&&{"code":0,"location":"https://platform.xiaomimimo.com/sts?nonce=123&ssecurity=sec","nonce":123,"ssecurity":"sec","userId":"42"}`, nil), nil
		case req.URL.Host == "platform.xiaomimimo.com" && req.URL.Path == "/sts":
			headers := http.Header{}
			headers.Set("Location", "/api/v1/userProfile")
			for _, cookie := range []string{
				"api-platform_serviceToken=fresh; Path=/; Domain=.platform.xiaomimimo.com",
				"api-platform_slh=slh; Path=/; Domain=.platform.xiaomimimo.com",
				"api-platform_ph=ph; Path=/; Domain=.platform.xiaomimimo.com",
				"userId=42; Path=/; Domain=.xiaomimimo.com",
			} {
				headers.Add("Set-Cookie", cookie)
			}
			refreshed = true
			return testResponse(http.StatusFound, "", headers), nil
		case req.URL.Host == "platform.xiaomimimo.com" && req.URL.Path == "/api/v1/userProfile":
			return testResponse(http.StatusOK, `{}`, nil), nil
		case req.URL.Host == "platform.xiaomimimo.com" && req.URL.Path == "/api/v1/balance":
			if !refreshed {
				return testResponse(http.StatusFound, "", nil), nil
			}
			return testResponse(http.StatusOK, `{"code":0,"data":{"balance":12.5,"currency":"CNY","cashBalance":10,"giftBalance":2.5}}`, nil), nil
		case req.URL.Host == "platform.xiaomimimo.com" && req.URL.Path == "/api/v1/tokenPlan/detail":
			if !refreshed {
				return testResponse(http.StatusFound, "", nil), nil
			}
			return testResponse(http.StatusOK, `{"code":0,"data":{"planCode":"lite"}}`, nil), nil
		case req.URL.Host == "platform.xiaomimimo.com" && req.URL.Path == "/api/v1/tokenPlan/usage":
			if !refreshed {
				return testResponse(http.StatusFound, "", nil), nil
			}
			return testResponse(http.StatusOK, `{"code":0,"data":{"monthUsage":{"items":[{"used":10,"limit":100}]}}}`, nil), nil
		}
		return testResponse(http.StatusNotFound, "", nil), nil
	})

	fetcher := &MiMo{
		Cookie:        "api-platform_serviceToken=stale; userId=42",
		AccountCookie: "userId=42; passToken=secret",
		Client:        &http.Client{Transport: transport},
	}
	snapshot, err := fetcher.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Credits != "Balance: ¥12.50" || snapshot.Source != "session cookie (auto-refreshed)" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if len(snapshot.Windows) != 1 || snapshot.Windows[0].Label != "monthly lite" {
		t.Fatalf("windows=%+v", snapshot.Windows)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, cookie := range seenCookies {
		if strings.Contains(cookie, "api-platform_serviceToken=fresh") {
			found = true
		}
	}
	if !found {
		t.Fatalf("refreshed cookie was not reused: %v", seenCookies)
	}
}

func TestMiMoFetchExpiredWithoutSeedExplainsFix(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(http.StatusFound, "", nil), nil
	})
	fetcher := &MiMo{Cookie: "api-platform_serviceToken=stale; userId=42", Client: &http.Client{Transport: transport}}
	_, err := fetcher.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session expired") || !strings.Contains(err.Error(), "providers.mimo.cookie") {
		t.Fatalf("err=%v", err)
	}
}

func TestMiMoFetchReportsRejectedAccountSeed(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/api/v1/genLoginUrl":
			headers := http.Header{}
			headers.Set("Location", testMiMoLoginURL)
			return testResponse(http.StatusFound, "", headers), nil
		case req.URL.Host == "account.xiaomi.com" && req.URL.Path == "/pass/serviceLogin":
			return testResponse(http.StatusOK, `&&&START&&&{"code":70016,"desc":"invalid session"}`, nil), nil
		}
		return testResponse(http.StatusFound, "", nil), nil
	})
	fetcher := &MiMo{
		Cookie:        "api-platform_serviceToken=stale; userId=42",
		AccountCookie: "userId=42; passToken=revoked",
		Client:        &http.Client{Transport: transport},
	}
	_, err := fetcher.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "automatic refresh failed") || !strings.Contains(err.Error(), "providers.mimo.account_cookie") {
		t.Fatalf("err=%v", err)
	}
}

func TestMiMoFetchBootstrapsFromAccountSeedOnly(t *testing.T) {
	refreshed := false
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/api/v1/genLoginUrl":
			headers := http.Header{}
			headers.Set("Location", testMiMoLoginURL)
			return testResponse(http.StatusFound, "", headers), nil
		case req.URL.Host == "account.xiaomi.com" && req.URL.Path == "/pass/serviceLogin":
			return testResponse(http.StatusOK, `&&&START&&&{"code":0,"location":"https://platform.xiaomimimo.com/sts?nonce=1&ssecurity=s","nonce":1,"ssecurity":"s"}`, nil), nil
		case req.URL.Path == "/sts":
			headers := http.Header{}
			headers.Set("Location", "/api/v1/userProfile")
			for _, cookie := range []string{
				"api-platform_serviceToken=fresh; Path=/",
				"api-platform_slh=slh; Path=/",
				"api-platform_ph=ph; Path=/",
				"userId=42; Path=/",
			} {
				headers.Add("Set-Cookie", cookie)
			}
			refreshed = true
			return testResponse(http.StatusFound, "", headers), nil
		case req.URL.Path == "/api/v1/userProfile":
			return testResponse(http.StatusOK, `{}`, nil), nil
		case req.URL.Path == "/api/v1/balance":
			if !refreshed {
				return testResponse(http.StatusFound, "", nil), nil
			}
			return testResponse(http.StatusOK, `{"code":0,"data":{"balance":1,"currency":"USD"}}`, nil), nil
		case req.URL.Path == "/api/v1/tokenPlan/detail", req.URL.Path == "/api/v1/tokenPlan/usage":
			if !refreshed {
				return testResponse(http.StatusFound, "", nil), nil
			}
			return testResponse(http.StatusOK, `{"code":0,"data":{}}`, nil), nil
		}
		return testResponse(http.StatusNotFound, "", nil), nil
	})
	fetcher := &MiMo{AccountCookie: "userId=42; passToken=secret", Client: &http.Client{Transport: transport}}
	snapshot, err := fetcher.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Credits != "Balance: $1.00" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}
