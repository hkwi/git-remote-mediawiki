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

func TestEditPageMinor(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case "GET":
			return jsonResponse(`{"query":{"tokens":{"csrftoken":"tok123"}}}`, req), nil
		case "POST":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			values := string(body)
			if !strings.Contains(values, "minor=1") {
				t.Fatalf("missing minor flag: %q", values)
			}
			return jsonResponse(`{"edit":{"newrevid":123}}`, req), nil
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = oldTransport }()

	revid, err := EditPage("http://example.com/api.php", nil, "Foo", "Body", "Summary", true)
	if err != nil {
		t.Fatalf("EditPage failed: %v", err)
	}
	if revid != 123 {
		t.Fatalf("unexpected revid: %d", revid)
	}
}

func TestEditPageReturnsMediaWikiAPIError(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case "GET":
			return jsonResponse(`{"query":{"tokens":{"csrftoken":"tok123"}}}`, req), nil
		case "POST":
			return jsonResponse(`{"error":{"code":"permissiondenied","info":"You do not have permission to edit this page."}}`, req), nil
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = oldTransport }()

	_, err := EditPage("http://example.com/api.php", nil, "Foo", "Body", "Summary", false)
	if err == nil || !strings.Contains(err.Error(), "permissiondenied") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestEditPageReturnsFailedResult(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case "GET":
			return jsonResponse(`{"query":{"tokens":{"csrftoken":"tok123"}}}`, req), nil
		case "POST":
			return jsonResponse(`{"edit":{"result":"Failure"}}`, req), nil
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = oldTransport }()

	_, err := EditPage("http://example.com/api.php", nil, "Foo", "Body", "Summary", false)
	if err == nil || !strings.Contains(err.Error(), "edit failed") {
		t.Fatalf("expected edit failure, got %v", err)
	}
}

func TestDeletePageReturnsMediaWikiAPIError(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case "GET":
			return jsonResponse(`{"query":{"tokens":{"csrftoken":"tok123"}}}`, req), nil
		case "POST":
			return jsonResponse(`{"error":{"code":"missingtitle","info":"The page you specified does not exist."}}`, req), nil
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = oldTransport }()

	_, err := DeletePage("http://example.com/api.php", nil, "Foo", "Deleted from git")
	if err == nil || !strings.Contains(err.Error(), "missingtitle") {
		t.Fatalf("expected API error, got %v", err)
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

func TestUploadFileMinor(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case "GET":
			return jsonResponse(`{"query":{"tokens":{"csrftoken":"tok123"}}}`, req), nil
		case "POST":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			values := string(body)
			if !strings.Contains(values, "name=\"minor\"") || !strings.Contains(values, "\r\n1\r\n") {
				t.Fatalf("missing minor field: %q", values)
			}
			if !strings.Contains(values, "name=\"ignoreminorerror\"") {
				t.Fatalf("missing ignoreminorerror field: %q", values)
			}
			return jsonResponse(`{"upload":{"result":"Success","imageinfo":{"timestamp":"2024-01-02T03:04:05Z"}}}`, req), nil
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = oldTransport }()

	revid, err := UploadFile("http://example.com/api.php", nil, "Foo.txt", []byte("hello"), "Upload", true)
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}
	if revid != 1 {
		t.Fatalf("unexpected upload marker: %d", revid)
	}
}

func TestUploadFileReturnsMediaWikiAPIError(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case "GET":
			return jsonResponse(`{"query":{"tokens":{"csrftoken":"tok123"}}}`, req), nil
		case "POST":
			return jsonResponse(`{"error":{"code":"fileexists-no-change","info":"No changes were made."}}`, req), nil
		default:
			t.Fatalf("unexpected method: %s", req.Method)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = oldTransport }()

	_, err := UploadFile("http://example.com/api.php", nil, "Foo.txt", []byte("hello"), "Upload", false)
	if err == nil || !strings.Contains(err.Error(), "fileexists-no-change") {
		t.Fatalf("expected API error, got %v", err)
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
