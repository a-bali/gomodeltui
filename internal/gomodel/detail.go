package gomodel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// RequestDetail resolves the persisted audit ID when a live event supplied only
// the logical request ID. Search results must match that identity exactly.
func (c *Client) RequestDetail(ctx context.Context, logID, requestID string) (Event, error) {
	if logID == "" {
		query := url.Values{"search": {requestID}, "limit": {"100"}}
		previous := ""
		for offset := 0; ; {
			query.Set("offset", strconv.Itoa(offset))
			resp, err := c.do(ctx, "/admin/audit/log?"+query.Encode(), nil)
			if err != nil {
				return Event{}, err
			}
			var page struct {
				Entries []struct {
					ID        string `json:"id"`
					RequestID string `json:"request_id"`
				} `json:"entries"`
				Total int `json:"total"`
			}
			err = json.NewDecoder(resp.Body).Decode(&page)
			resp.Body.Close()
			if err != nil {
				return Event{}, fmt.Errorf("decode audit lookup: %w", err)
			}
			for _, entry := range page.Entries {
				if entry.RequestID == requestID && entry.ID != "" {
					logID = entry.ID
					break
				}
			}
			if logID != "" {
				break
			}
			offset += len(page.Entries)
			if len(page.Entries) == 0 || offset >= page.Total {
				return Event{}, fmt.Errorf("request is not yet available in persisted audit history")
			}
			last := page.Entries[len(page.Entries)-1].ID
			if last == previous {
				return Event{}, fmt.Errorf("audit lookup stopped making progress")
			}
			previous = last
		}
	}
	event, err := c.AuditLogDetail(ctx, logID)
	if err != nil {
		return Event{}, err
	}
	var envelope struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(event.Data, &envelope); err != nil || envelope.RequestID != requestID {
		return Event{}, fmt.Errorf("audit detail does not match the selected request")
	}
	return event, nil
}

type RequestContent struct {
	AuditLogID string
	Preview    string
	RawJSON    string
	Metadata   Request
}

func ContentFromDetail(event Event) (RequestContent, error) {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(event.Data, &envelope); err != nil {
		return RequestContent{}, err
	}
	raw := prettyJSON(event.Data)
	var usage Request
	mergeRequestUsage(&usage, envelope.Data, false)
	usage.captureMCP(raw)
	return RequestContent{AuditLogID: firstString(envelope.Data["id"]), Preview: previewFromFields(envelope.Data), RawJSON: raw, Metadata: usage}, nil
}

func previewFromFields(fields map[string]any) string {
	text := lastTurn(fields["data"])
	if text == "" {
		text = lastResponseTurn(fields["data"])
	}
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > 4096 {
		runes = runes[:4096]
	}
	return string(runes)
}

// EnrichContent fills missing fields without replaying a terminal event.
func (r *Reducer) EnrichContent(id string, content RequestContent) {
	if request := r.requests[id]; request != nil {
		request.EnrichContent(content)
	}
}

func (r *Request) EnrichContent(content RequestContent) {
	if r.LastTurn == "" {
		r.LastTurn = content.Preview
	}
	if r.AuditLogID == "" {
		r.AuditLogID = content.AuditLogID
	}
	if !r.InputTokensKnown && content.Metadata.InputTokensKnown {
		r.InputTokens, r.InputTokensKnown = content.Metadata.InputTokens, true
	}
	if !r.OutputTokensKnown && content.Metadata.OutputTokensKnown {
		r.OutputTokens, r.OutputTokensKnown = content.Metadata.OutputTokens, true
	}
	if !r.CacheRatioKnown && content.Metadata.CacheRatioKnown {
		r.CacheRatio, r.CacheRatioKnown = content.Metadata.CacheRatio, true
	}
	if r.IsMCP() {
		if content.Metadata.MCPActionLabel != "" {
			r.MCPActionLabel = content.Metadata.MCPActionLabel
		}
		if content.Metadata.MCPResultKnown {
			r.MCPResultKnown, r.MCPFailed = true, content.Metadata.MCPFailed
		}
	}
}
