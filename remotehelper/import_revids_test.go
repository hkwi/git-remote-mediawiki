package remotehelper

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git-remote-mediawiki/client"
)

func TestImportRevidsFullImportResetsTargetRef(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	err := importRevids(
		out,
		errOut,
		"origin",
		"http://example.com/api.php",
		nil,
		[]int{},
		map[string]bool{},
		1,
	)
	if err != nil {
		t.Fatalf("importRevids failed: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "reset refs/mediawiki/origin/master") {
		t.Fatalf("unexpected reset emitted without revisions: %q", got)
	}
}

func TestImportRevidsFullImportFirstCommitResetsRefs(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"query": {
				"pages": [
					{
						"title": "Test Page",
						"revisions": [
							{
								"revid": 42,
								"timestamp": "2024-01-02T03:04:05Z",
								"user": "Alice",
								"comment": "Imported",
								"content": "Hello"
							}
						]
					}
				]
			}
		}`
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	defer func() { http.DefaultTransport = oldTransport }()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	err := importRevids(
		out,
		errOut,
		"origin",
		"http://example.com/api.php",
		nil,
		[]int{42},
		map[string]bool{"Test Page": true},
		1,
	)
	if err != nil {
		t.Fatalf("importRevids failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "reset refs/mediawiki/origin/master\ncommit refs/mediawiki/origin/master\n") {
		t.Fatalf("missing reset before first commit: %q", got)
	}
	if !strings.Contains(got, "reset refs/notes/commits\ncommit refs/notes/commits\n") {
		t.Fatalf("missing legacy notes reset before first note commit: %q", got)
	}
	if !strings.Contains(got, "reset refs/notes/origin/mediawiki\ncommit refs/notes/origin/mediawiki\n") {
		t.Fatalf("missing notes reset before first note commit: %q", got)
	}
}

func TestImportRevidsSanitizesCommitterForFastImport(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"query": {
				"pages": [
					{
						"title": "Test Page",
						"revisions": [
							{
								"revid": 42,
								"timestamp": "2024-01-02T03:04:05Z",
								"user": "Alice <admin>\nroot",
								"comment": "Imported",
								"content": "Hello"
							}
						]
					}
				]
			}
		}`
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	defer func() { http.DefaultTransport = oldTransport }()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	err := importRevids(
		out,
		errOut,
		"origin",
		"http://Example.COM:8080/api.php",
		nil,
		[]int{42},
		map[string]bool{"Test Page": true},
		1,
	)
	if err != nil {
		t.Fatalf("importRevids failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "committer Alice admin root <alice__admin__root@example.com-8080> 1704164645 +0000\n") {
		t.Fatalf("committer line was not sanitized as expected: %q", got)
	}

	repo := t.TempDir()
	cmd := exec.Command("git", "init", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	fastImport := exec.Command("git", "fast-import", "--quiet")
	fastImport.Dir = repo
	fastImport.Stdin = strings.NewReader(got)
	if out, err := fastImport.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import rejected sanitized stream: %v\n%s\nstream:\n%s", err, out, got)
	}

	if _, err := os.Stat(filepath.Join(repo, ".git", "refs", "mediawiki", "origin", "master")); err != nil {
		t.Fatalf("expected imported ref to exist: %v", err)
	}
}

func TestImportRevidsFallsBackToSnapshotWhenNoTrackedRevisionsMatched(t *testing.T) {
	oldTransport := http.DefaultTransport
	oldGetPages := getPagesByTitlesWithClientFunc
	defer func() {
		http.DefaultTransport = oldTransport
		getPagesByTitlesWithClientFunc = oldGetPages
	}()

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"query": {
				"pages": [
					{
						"title": "Deleted Page",
						"revisions": [
							{
								"revid": 42,
								"timestamp": "2024-01-02T03:04:05Z",
								"user": "Alice",
								"comment": "Imported",
								"content": "Hello"
							}
						]
					}
				]
			}
		}`
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
	getPagesByTitlesWithClientFunc = func(httpClient *http.Client, apiURL string, titles []string) ([]client.Page, error) {
		if len(titles) != 1 || titles[0] != "Main Page" {
			t.Fatalf("unexpected titles: %#v", titles)
		}
		return []client.Page{{Title: "Main Page", Content: "Welcome"}}, nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	err := importRevids(
		out,
		errOut,
		"origin",
		"http://example.com/api.php",
		nil,
		[]int{42},
		map[string]bool{"Main Page": true},
		1,
	)
	if err != nil {
		t.Fatalf("importRevids failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "commit refs/mediawiki/origin/master\n") {
		t.Fatalf("missing fallback snapshot commit: %q", got)
	}
	if !strings.Contains(got, "M 644 inline \"Main_Page.mw\"\n") {
		t.Fatalf("missing tracked page in fallback snapshot: %q", got)
	}
	if strings.Contains(got, "Deleted_Page.mw") {
		t.Fatalf("unexpected untracked deleted page import: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
