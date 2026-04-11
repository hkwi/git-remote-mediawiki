package client

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRetryAfter = 5 * time.Minute

var retryAfterSleep = time.Sleep
var retryAfterNow = time.Now

// DoRequestWithRetry executes an HTTP request and retries 429 responses only
// when the server provides a Retry-After header that does not exceed 5 minutes.
func DoRequestWithRetry(httpClient *http.Client, newRequest func() (*http.Request, error)) (*http.Response, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	for {
		req, err := newRequest()
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		delay, ok, err := parseRetryAfter(resp.Header.Get("Retry-After"), retryAfterNow())
		if err != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP 429 with invalid Retry-After header %q: %w; body: %s", resp.Header.Get("Retry-After"), err, string(body))
		}
		if !ok {
			return resp, nil
		}
		if delay > maxRetryAfter {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP 429 Retry-After %s exceeds %s: %s", delay, maxRetryAfter, string(body))
		}
		resp.Body.Close()
		retryAfterSleep(delay)
	}
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0, false, fmt.Errorf("negative duration")
		}
		return time.Duration(secs) * time.Second, true, nil
	}
	t, err := http.ParseTime(value)
	if err != nil {
		return 0, false, err
	}
	if t.Before(now) {
		return 0, true, nil
	}
	return t.Sub(now), true, nil
}
