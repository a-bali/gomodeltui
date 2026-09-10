package gomodel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("GoModel URL is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("GoModel token is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, token: token, http: httpClient}, nil
}

func (c *Client) do(ctx context.Context, path string, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("GoModel %s returned HTTP %d", path, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) Health(ctx context.Context) error {
	resp, err := c.do(ctx, "/health/ready", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

func (c *Client) LiveLogs(ctx context.Context, lastEventID string) (*http.Response, error) {
	headers := http.Header{"Accept": []string{"text/event-stream"}}
	if strings.TrimSpace(lastEventID) != "" {
		headers.Set("Last-Event-ID", lastEventID)
	}
	return c.do(ctx, "/admin/live/logs", headers)
}
