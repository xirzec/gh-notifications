package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// rateLimit captures the GitHub REST API quota reported in response headers.
type rateLimit struct {
	remaining int
	limit     int
	reset     time.Time
}

// parseRateLimit extracts the rate limit from REST response headers. It reports
// false when the relevant headers are absent or malformed.
func parseRateLimit(h http.Header) (rateLimit, bool) {
	remStr := h.Get("X-RateLimit-Remaining")
	limStr := h.Get("X-RateLimit-Limit")
	if remStr == "" || limStr == "" {
		return rateLimit{}, false
	}
	remaining, err1 := strconv.Atoi(remStr)
	limit, err2 := strconv.Atoi(limStr)
	if err1 != nil || err2 != nil {
		return rateLimit{}, false
	}
	rl := rateLimit{remaining: remaining, limit: limit}
	if resetStr := h.Get("X-RateLimit-Reset"); resetStr != "" {
		if sec, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			rl.reset = time.Unix(sec, 0)
		}
	}
	return rl, true
}

// rateLimitTracker wraps a requestDoer and records the rate limit reported by
// the most recent successful REST response. It also stores the GraphQL quota
// when one is observed. It is safe for concurrent use, as requests may be issued
// from the interactive picker's background commands.
type rateLimitTracker struct {
	doer requestDoer
	mu   sync.Mutex
	last *rateLimit
	gql  *rateLimit
}

func newRateLimitTracker(doer requestDoer) *rateLimitTracker {
	return &rateLimitTracker{doer: doer}
}

func (t *rateLimitTracker) Request(method, path string, body io.Reader) (*http.Response, error) {
	resp, err := t.doer.Request(method, path, body)
	if resp != nil {
		if rl, ok := parseRateLimit(resp.Header); ok {
			t.mu.Lock()
			t.last = &rl
			t.mu.Unlock()
		}
	}
	return resp, err
}

// setGraphQL records the GraphQL API quota to be reported on exit.
func (t *rateLimitTracker) setGraphQL(rl *rateLimit) {
	t.mu.Lock()
	t.gql = rl
	t.mu.Unlock()
}

// reportLine writes a single quota line for rl, or nothing when rl is nil.
func reportLine(out io.Writer, label string, rl *rateLimit) {
	if rl == nil {
		return
	}
	resetStr := ""
	if !rl.reset.IsZero() {
		resetStr = fmt.Sprintf(", resets at %s", rl.reset.Local().Format("15:04:05"))
	}
	fmt.Fprintf(out, "%s quota: %d/%d remaining%s\n", label, rl.remaining, rl.limit, resetStr)
}

// report writes the most recently observed REST and GraphQL rate limits to out.
// It does nothing for a quota that has not been observed.
func (t *rateLimitTracker) report(out io.Writer) {
	t.mu.Lock()
	rest, gql := t.last, t.gql
	t.mu.Unlock()
	reportLine(out, "GitHub REST API", rest)
	reportLine(out, "GitHub GraphQL API", gql)
}
