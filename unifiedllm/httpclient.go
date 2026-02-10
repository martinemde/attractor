package unifiedllm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// httpClient is a shared HTTP client wrapper with configurable timeouts.
type httpClient struct {
	client *http.Client
}

// newHTTPClient creates an HTTP client with default timeouts.
func newHTTPClient() *httpClient {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second, // connect timeout
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout:  120 * time.Second,
		MaxIdleConns:           100,
		MaxIdleConnsPerHost:    10,
		IdleConnTimeout:        90 * time.Second,
	}
	return &httpClient{
		client: &http.Client{
			Transport: transport,
			Timeout:   120 * time.Second, // request timeout
		},
	}
}

// Do executes an HTTP request.
func (hc *httpClient) Do(req *http.Request) (*http.Response, error) {
	return hc.client.Do(req)
}

// sseEvent represents a single Server-Sent Event.
type sseEvent struct {
	Event string
	Data  string
}

// sseReader parses SSE streams from an io.Reader.
type sseReader struct {
	scanner *bufio.Scanner
}

// newSSEReader creates a new SSE parser.
func newSSEReader(r io.Reader) *sseReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	return &sseReader{scanner: scanner}
}

// Next returns the next SSE event. Returns io.EOF when the stream ends.
// Returns a special sseEvent with Event="[DONE]" for OpenAI-style termination.
func (r *sseReader) Next() (*sseEvent, error) {
	var event sseEvent
	var dataLines []string
	hasData := false

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Blank line = event boundary
		if line == "" {
			if hasData {
				event.Data = strings.Join(dataLines, "\n")
				return &event, nil
			}
			continue
		}

		// Comment lines (starting with :) are ignored
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ") // trim optional single leading space
			// Check for [DONE] termination
			if data == "[DONE]" {
				return &sseEvent{Event: "[DONE]", Data: "[DONE]"}, nil
			}
			dataLines = append(dataLines, data)
			hasData = true
		} else if strings.HasPrefix(line, "retry:") {
			// Retry directives are ignored for now
			continue
		}
	}

	if err := r.scanner.Err(); err != nil {
		return nil, err
	}

	// If we had accumulated data when stream ended, return it
	if hasData {
		event.Data = strings.Join(dataLines, "\n")
		return &event, nil
	}

	return nil, io.EOF
}

// parseRetryAfter parses a Retry-After header value.
// Supports both seconds (integer) and HTTP-date formats.
func parseRetryAfter(value string) *float64 {
	if value == "" {
		return nil
	}

	// Try parsing as seconds (integer or float)
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return &seconds
	}

	// Try parsing as HTTP-date (RFC 1123 format)
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		seconds := time.Until(t).Seconds()
		if seconds < 0 {
			seconds = 0
		}
		return &seconds
	}

	// Try RFC 850 format
	if t, err := time.Parse(time.RFC850, value); err == nil {
		seconds := time.Until(t).Seconds()
		if seconds < 0 {
			seconds = 0
		}
		return &seconds
	}

	return nil
}

// parseRateLimitHeaders extracts rate limit info from response headers.
func parseRateLimitHeaders(headers http.Header) *RateLimitInfo {
	info := &RateLimitInfo{}
	hasAny := false

	if v := headers.Get("x-ratelimit-remaining-requests"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.RequestsRemaining = &n
			hasAny = true
		}
	}
	if v := headers.Get("x-ratelimit-limit-requests"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.RequestsLimit = &n
			hasAny = true
		}
	}
	if v := headers.Get("x-ratelimit-remaining-tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.TokensRemaining = &n
			hasAny = true
		}
	}
	if v := headers.Get("x-ratelimit-limit-tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.TokensLimit = &n
			hasAny = true
		}
	}
	if v := headers.Get("x-ratelimit-reset-requests"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			info.ResetAt = &t
			hasAny = true
		} else if d, err := time.ParseDuration(v); err == nil {
			t := time.Now().Add(d)
			info.ResetAt = &t
			hasAny = true
		}
	}

	if !hasAny {
		return nil
	}
	return info
}

// buildErrorFromResponse creates an appropriate error from an HTTP response.
func buildErrorFromResponse(resp *http.Response, providerName string) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &NetworkError{SDKError: SDKError{
			Message: fmt.Sprintf("failed to read error response body: %v", err),
			Cause:   err,
		}}
	}

	var raw map[string]interface{}
	var message, errorCode string

	if err := json.Unmarshal(body, &raw); err == nil {
		// Try standard error formats
		if errObj, ok := raw["error"].(map[string]interface{}); ok {
			if msg, ok := errObj["message"].(string); ok {
				message = msg
			}
			if code, ok := errObj["code"].(string); ok {
				errorCode = code
			}
			if code, ok := errObj["type"].(string); ok && errorCode == "" {
				errorCode = code
			}
		}
		// Anthropic format: {"type": "error", "error": {"type": "...", "message": "..."}}
		if message == "" {
			if errObj, ok := raw["error"].(map[string]interface{}); ok {
				if msg, ok := errObj["message"].(string); ok {
					message = msg
				}
				if etype, ok := errObj["type"].(string); ok {
					errorCode = etype
				}
			}
		}
		// Gemini format: {"error": {"message": "...", "status": "..."}}
		if message == "" {
			if msg, ok := raw["message"].(string); ok {
				message = msg
			}
		}
	}

	if message == "" {
		message = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	return ErrorFromStatusCode(resp.StatusCode, message, providerName, errorCode, raw, retryAfter)
}
