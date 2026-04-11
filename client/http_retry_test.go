package client

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoRequestWithRetryRetriesOn429WithShortRetryAfter(t *testing.T) {
	oldTransport := http.DefaultTransport
	oldSleep := retryAfterSleep
	defer func() {
		http.DefaultTransport = oldTransport
		retryAfterSleep = oldSleep
	}()

	attempts := 0
	var slept time.Duration
	retryAfterSleep = func(d time.Duration) {
		slept += d
	}
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"1"}},
				Body:       io.NopCloser(strings.NewReader("slow down")),
				Request:    req,
			}, nil
		}
		return jsonResponse(`{"query":{"allpages":[{"title":"P1"}]}}`, req), nil
	})

	resp, err := DoRequestWithRetry(nil, func() (*http.Request, error) {
		return http.NewRequest("GET", "http://example.com/api.php", nil)
	})
	if err != nil {
		t.Fatalf("DoRequestWithRetry failed: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 2 {
		t.Fatalf("unexpected attempts: got %d want 2", attempts)
	}
	if slept != time.Second {
		t.Fatalf("unexpected sleep duration: got %s want %s", slept, time.Second)
	}
}

func TestDoRequestWithRetryRejectsLongRetryAfter(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"301"}},
			Body:       io.NopCloser(strings.NewReader("too long")),
			Request:    req,
		}, nil
	})

	_, err := DoRequestWithRetry(nil, func() (*http.Request, error) {
		return http.NewRequest("GET", "http://example.com/api.php", nil)
	})
	if err == nil {
		t.Fatal("expected error for long Retry-After")
	}
	if !strings.Contains(err.Error(), "exceeds 5m0s") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryAllPagesRetriesOn429WithRetryAfter(t *testing.T) {
	oldTransport := http.DefaultTransport
	oldSleep := retryAfterSleep
	defer func() {
		http.DefaultTransport = oldTransport
		retryAfterSleep = oldSleep
	}()

	attempts := 0
	retryAfterSleep = func(time.Duration) {}
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"0"}},
				Body:       io.NopCloser(strings.NewReader("wait")),
				Request:    req,
			}, nil
		}
		return jsonResponse(`{"query":{"allpages":[{"pageid":1,"title":"P1"}]}}`, req), nil
	})

	pages, err := QueryAllPages("http://example.com/api.php", 0, 1)
	if err != nil {
		t.Fatalf("QueryAllPages failed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("unexpected attempts: got %d want 2", attempts)
	}
	if len(pages) != 1 || pages[0]["title"] != "P1" {
		t.Fatalf("unexpected pages: %#v", pages)
	}
}
