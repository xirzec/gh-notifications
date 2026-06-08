package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseRateLimit(t *testing.T) {
	t.Run("valid headers", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "4985")
		h.Set("X-RateLimit-Limit", "5000")
		h.Set("X-RateLimit-Reset", "1700000000")
		rl, ok := parseRateLimit(h)
		if !ok {
			t.Fatal("expected ok")
		}
		if rl.remaining != 4985 || rl.limit != 5000 {
			t.Errorf("got %+v", rl)
		}
		if rl.reset != time.Unix(1700000000, 0) {
			t.Errorf("reset = %v", rl.reset)
		}
	})

	t.Run("missing headers", func(t *testing.T) {
		if _, ok := parseRateLimit(http.Header{}); ok {
			t.Error("expected ok=false without headers")
		}
	})

	t.Run("malformed remaining", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "abc")
		h.Set("X-RateLimit-Limit", "5000")
		if _, ok := parseRateLimit(h); ok {
			t.Error("expected ok=false for malformed value")
		}
	})

	t.Run("no reset header", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "10")
		h.Set("X-RateLimit-Limit", "60")
		rl, ok := parseRateLimit(h)
		if !ok || !rl.reset.IsZero() {
			t.Errorf("got %+v ok=%v", rl, ok)
		}
	})
}

// headerDoer returns responses with the configured headers, in sequence.
type headerDoer struct {
	headers []http.Header
	i       int
}

func (d *headerDoer) Request(method, path string, body io.Reader) (*http.Response, error) {
	h := http.Header{}
	if d.i < len(d.headers) {
		h = d.headers[d.i]
	}
	d.i++
	return &http.Response{Body: io.NopCloser(strings.NewReader("")), Header: h}, nil
}

func header(remaining, limit string) http.Header {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", remaining)
	h.Set("X-RateLimit-Limit", limit)
	return h
}

func TestRateLimitTrackerRecordsLatest(t *testing.T) {
	doer := &headerDoer{headers: []http.Header{
		header("100", "5000"),
		header("98", "5000"),
	}}
	tracker := newRateLimitTracker(doer)

	if _, err := tracker.Request(http.MethodGet, "a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Request(http.MethodGet, "b", nil); err != nil {
		t.Fatal(err)
	}

	if tracker.last == nil || tracker.last.remaining != 98 {
		t.Errorf("expected last remaining 98, got %+v", tracker.last)
	}
}

func TestRateLimitReport(t *testing.T) {
	t.Run("no data prints nothing", func(t *testing.T) {
		var buf bytes.Buffer
		newRateLimitTracker(&headerDoer{}).report(&buf)
		if buf.Len() != 0 {
			t.Errorf("expected no output, got %q", buf.String())
		}
	})

	t.Run("prints remaining and limit", func(t *testing.T) {
		tracker := newRateLimitTracker(&headerDoer{})
		tracker.last = &rateLimit{remaining: 4985, limit: 5000}
		var buf bytes.Buffer
		tracker.report(&buf)
		out := buf.String()
		if !strings.Contains(out, "4985/5000 remaining") {
			t.Errorf("unexpected output %q", out)
		}
	})

	t.Run("includes reset time when set", func(t *testing.T) {
		tracker := newRateLimitTracker(&headerDoer{})
		tracker.last = &rateLimit{remaining: 1, limit: 60, reset: time.Unix(1700000000, 0)}
		var buf bytes.Buffer
		tracker.report(&buf)
		if !strings.Contains(buf.String(), "resets at") {
			t.Errorf("expected reset time, got %q", buf.String())
		}
	})
}
