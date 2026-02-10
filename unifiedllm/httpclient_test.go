package unifiedllm

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEReaderBasic(t *testing.T) {
	input := "data: hello\n\ndata: world\n\n"
	reader := newSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "hello", event.Data)

	event, err = reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "world", event.Data)
}

func TestSSEReaderEventType(t *testing.T) {
	input := "event: message\ndata: {\"text\":\"hello\"}\n\n"
	reader := newSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "message", event.Event)
	assert.Equal(t, `{"text":"hello"}`, event.Data)
}

func TestSSEReaderDone(t *testing.T) {
	input := "data: some text\n\ndata: [DONE]\n\n"
	reader := newSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "some text", event.Data)

	event, err = reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "[DONE]", event.Event)
}

func TestSSEReaderMultiLineData(t *testing.T) {
	input := "data: line1\ndata: line2\n\n"
	reader := newSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2", event.Data)
}

func TestSSEReaderIgnoresComments(t *testing.T) {
	input := ": this is a comment\ndata: actual data\n\n"
	reader := newSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "actual data", event.Data)
}

func TestSSEReaderIgnoresRetry(t *testing.T) {
	input := "retry: 3000\ndata: hello\n\n"
	reader := newSSEReader(strings.NewReader(input))

	event, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "hello", event.Data)
}

func TestParseRetryAfterSeconds(t *testing.T) {
	result := parseRetryAfter("30")
	require.NotNil(t, result)
	assert.Equal(t, float64(30), *result)
}

func TestParseRetryAfterFloat(t *testing.T) {
	result := parseRetryAfter("1.5")
	require.NotNil(t, result)
	assert.Equal(t, 1.5, *result)
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	futureDate := time.Now().Add(60 * time.Second).UTC().Format(time.RFC1123)
	result := parseRetryAfter(futureDate)
	require.NotNil(t, result)
	assert.Greater(t, *result, float64(50))
}

func TestParseRetryAfterEmpty(t *testing.T) {
	result := parseRetryAfter("")
	assert.Nil(t, result)
}

func TestParseRetryAfterInvalid(t *testing.T) {
	result := parseRetryAfter("not-a-number-or-date")
	assert.Nil(t, result)
}

func TestParseRateLimitHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-ratelimit-remaining-requests", "99")
	headers.Set("x-ratelimit-limit-requests", "100")
	headers.Set("x-ratelimit-remaining-tokens", "9999")
	headers.Set("x-ratelimit-limit-tokens", "10000")

	info := parseRateLimitHeaders(headers)
	require.NotNil(t, info)
	assert.Equal(t, 99, *info.RequestsRemaining)
	assert.Equal(t, 100, *info.RequestsLimit)
	assert.Equal(t, 9999, *info.TokensRemaining)
	assert.Equal(t, 10000, *info.TokensLimit)
}

func TestParseRateLimitHeadersEmpty(t *testing.T) {
	headers := http.Header{}
	info := parseRateLimitHeaders(headers)
	assert.Nil(t, info)
}

func TestParseRateLimitHeadersPartial(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-ratelimit-remaining-requests", "50")

	info := parseRateLimitHeaders(headers)
	require.NotNil(t, info)
	assert.Equal(t, 50, *info.RequestsRemaining)
	assert.Nil(t, info.RequestsLimit)
}

func TestBuildErrorFromResponseJSON(t *testing.T) {
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{},
		Body:       newReadCloser(`{"error":{"message":"Rate limit exceeded","code":"rate_limit"}}`),
	}
	resp.Header.Set("Retry-After", "10")

	err := buildErrorFromResponse(resp, "test-provider")
	require.Error(t, err)

	var rlErr *RateLimitError
	require.ErrorAs(t, err, &rlErr)
	assert.Equal(t, "test-provider", rlErr.Provider)
	assert.Equal(t, 429, rlErr.StatusCode)
	assert.Contains(t, rlErr.Message, "Rate limit exceeded")
	require.NotNil(t, rlErr.RetryAfter)
	assert.Equal(t, float64(10), *rlErr.RetryAfter)
}

func TestBuildErrorFromResponsePlainText(t *testing.T) {
	resp := &http.Response{
		StatusCode: 500,
		Header:     http.Header{},
		Body:       newReadCloser("Internal Server Error"),
	}

	err := buildErrorFromResponse(resp, "test-provider")
	require.Error(t, err)

	var sErr *ServerError
	require.ErrorAs(t, err, &sErr)
}

// helper to create io.ReadCloser from string
type readCloserStr struct {
	*strings.Reader
}

func (r readCloserStr) Close() error { return nil }

func newReadCloser(s string) readCloserStr {
	return readCloserStr{strings.NewReader(s)}
}
