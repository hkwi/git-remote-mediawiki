package client

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDeletePage(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case "GET":
			if req.URL.Query().Get("action") != "query" || req.URL.Query().Get("meta") != "tokens" {
				t.Fatalf("unexpected token request: %s", req.URL.String())
			}
			return jsonResponse(`{"query":{"tokens":{"csrftoken":"tok123"}}}`, req), nil
		case "POST":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			values := string(body)
			if !strings.Contains(values, "action=delete") {
				t.Fatalf("missing delete action: %q", values)
			}
			if !strings.Contains(values, "title=Foo") {
				t.Fatalf("missing title: %q", values)
			}
			if !strings.Contains(values, "reason=Deleted+from+git") {
				t.Fatalf("missing reason: %q", values)
			}
			if !strings.Contains(values, "token=tok123") {
				t.Fatalf("missing token: %q", values)
			}
			return jsonResponse(`{"delete":{"title":"Foo","reason":"Deleted from git","logid":456}}`, req), nil
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = oldTransport }()

	revid, err := DeletePage("http://example.com/api.php", nil, "Foo", "Deleted from git")
	if err != nil {
		t.Fatalf("DeletePage failed: %v", err)
	}
	if revid != 456 {
		t.Fatalf("unexpected revid/logid: %d", revid)
	}
}

func TestGetFileURLAtTimestamp(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != "GET" {
			t.Fatalf("unexpected method: %s", req.Method)
		}
		q := req.URL.Query()
		if q.Get("action") != "query" || q.Get("prop") != "imageinfo" {
			t.Fatalf("unexpected query: %s", req.URL.String())
		}
		if q.Get("iistart") != "2024-01-02T03:04:05Z" || q.Get("iiend") != "2024-01-02T03:04:05Z" {
			t.Fatalf("unexpected timestamp params: %s", req.URL.String())
		}
		return jsonResponse(`{"query":{"pages":[{"title":"File:Foo.txt","imageinfo":[{"url":"http://files.example/Foo.txt","timestamp":"2024-01-02T03:04:05Z"}]}]}}`, req), nil
	})
	defer func() { http.DefaultTransport = oldTransport }()

	got, err := GetFileURLAtTimestamp("http://example.com/api.php", nil, "File:Foo.txt", "2024-01-02T03:04:05Z")
	if err != nil {
		t.Fatalf("GetFileURLAtTimestamp failed: %v", err)
	}
	if got != "http://files.example/Foo.txt" {
		t.Fatalf("unexpected URL: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body string, req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
