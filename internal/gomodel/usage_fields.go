package gomodel

import "math"

func object(value any) map[string]any { result, _ := value.(map[string]any); return result }

func mergeRequestUsage(request *Request, fields map[string]any, missingOnly bool) {
	data := object(fields["data"])
	response := object(data["response_body"])
	sources := []map[string]any{fields, object(fields["usage"]), object(data["usage"]), object(response["usage"])}
	find := func(keys ...string) (float64, bool) {
		for _, source := range sources {
			for _, key := range keys {
				if value, ok := firstFloat(source[key]); ok && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 {
					return value, true
				}
			}
		}
		return 0, false
	}
	if !missingOnly || !request.InputTokensKnown {
		if value, ok := find("input_tokens", "prompt_tokens"); ok && value < float64(int(^uint(0)>>1)) {
			request.InputTokens, request.InputTokensKnown = int(value), true
		}
	}
	if !missingOnly || !request.OutputTokensKnown {
		if value, ok := find("output_tokens", "completion_tokens"); ok && value < float64(int(^uint(0)>>1)) {
			request.OutputTokens, request.OutputTokensKnown = int(value), true
		}
	}
	if missingOnly && request.CacheRatioKnown {
		return
	}
	if value, ok := find("cached_input_ratio", "cache_ratio"); ok {
		request.CacheRatio, request.CacheRatioKnown = min(1, value), true
		return
	}
	cached, known := find("cached_input_tokens", "cache_read_input_tokens")
	for _, source := range sources {
		if known {
			break
		}
		for _, key := range []string{"input_tokens_details", "prompt_tokens_details"} {
			if value, ok := firstFloat(object(source[key])["cached_tokens"]); ok && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
				cached, known = value, true
				break
			}
		}
	}
	if known && request.InputTokensKnown {
		if request.InputTokens > 0 {
			request.CacheRatio, request.CacheRatioKnown = min(1, cached/float64(request.InputTokens)), true
		} else if cached == 0 {
			request.CacheRatio, request.CacheRatioKnown = 0, true
		}
	}
}
