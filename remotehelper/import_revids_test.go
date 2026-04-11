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

func TestImportRevidsWarnsWhenRequestedRevisionIsMissingOnFullImport(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"query": {
				"pages": [
					{
						"title": "Test Page",
						"revisions": [
							{
								"revid": 41,
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
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errOut.String(), "warning: failed to retrieve revision(s): [42]") {
		t.Fatalf("missing warning: %q", errOut.String())
	}
}

func TestImportRevidsSkipsWarningForNormalGapBadRevids(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	t.Setenv("GIT_REMOTE_MEDIAWIKI_DEBUG", "1")

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"query": {
				"badrevids": {
					"22": {
						"missing": true,
						"revid": 22
					}
				},
				"pages": [
					{
						"title": "Test Page",
						"revisions": [
							{
								"revid": 21,
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

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	err := importRevids(
		out,
		errOut,
		"origin",
		"http://example.com/api.php",
		nil,
		[]int{21, 22},
		map[string]bool{"Test Page": true},
		1,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(errOut.String(), "warning: failed to retrieve revision(s): [22]") {
		t.Fatalf("unexpected warning for normal gap: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "normal revision gaps skipped=[22]") {
		t.Fatalf("missing debug note for normal gap: %q", errOut.String())
	}
}

func TestImportRevidsRetriesMissingRevisionAndImportsIt(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	t.Setenv("GIT_REMOTE_MEDIAWIKI_DEBUG", "1")
	var retryCalls int

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		revids := req.URL.Query().Get("revids")
		var body string
		switch {
		case req.URL.Query().Get("prop") == "revisions" && revids == "41|42|43":
			body = `{
				"query": {
					"pages": [
						{
							"title": "Test Page",
							"revisions": [
								{
									"revid": 41,
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
		case req.URL.Query().Get("prop") == "revisions" && revids == "42|43":
			retryCalls++
			if retryCalls == 1 {
				body = `{
					"query": {
						"pages": [
							{
								"title": "Recovered Page",
								"revisions": []
							}
						]
					}
				}`
			} else {
				body = `{
					"query": {
						"pages": [
							{
								"title": "Recovered Page",
								"revisions": [
									{
										"revid": 42,
										"timestamp": "2024-01-02T03:04:06Z",
										"user": "Bob",
										"comment": "Recovered",
										"content": "World"
									},
									{
										"revid": 43,
										"timestamp": "2024-01-02T03:04:06Z",
										"user": "Bob",
										"comment": "Recovered",
										"content": "World"
									}
								]
							}
						]
					}
				}`
			}
		default:
			t.Fatalf("unexpected query: %q", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	err := importRevids(
		out,
		errOut,
		"origin",
		"http://example.com/api.php",
		nil,
		[]int{41, 42, 43},
		map[string]bool{"Test Page": true, "Recovered Page": true},
		1,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(errOut.String(), "warning: failed to retrieve revision(s): [42 43]") {
		t.Fatalf("unexpected warning after retry recovery: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "retrying missing revisions individually=[42 43]") {
		t.Fatalf("missing retry debug note: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "retryMissingRevisionRecords: attempt=2") {
		t.Fatalf("missing second retry attempt: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Recovered_Page.mw") {
		t.Fatalf("missing recovered page import: %q", out.String())
	}
}

func TestImportRevidsSplitsBatchWhenResultIsTruncated(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	t.Setenv("GIT_REMOTE_MEDIAWIKI_DEBUG", "1")

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		revids := req.URL.Query().Get("revids")
		var body string
		switch {
		case req.URL.Query().Get("prop") == "revisions" && revids == "41|42":
			body = `{
				"warnings": {
					"result": {
						"*": "This result was truncated because it would otherwise be larger than the limit of 8,388,608 bytes."
					}
				},
				"query": {
					"pages": [
						{
							"title": "Test Page",
							"revisions": [
								{
									"revid": 41,
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
		case req.URL.Query().Get("prop") == "revisions" && revids == "41":
			body = `{
				"query": {
					"pages": [
						{
							"title": "Test Page",
							"revisions": [
								{
									"revid": 41,
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
		case req.URL.Query().Get("prop") == "revisions" && revids == "42":
			body = `{
				"query": {
					"pages": [
						{
							"title": "Recovered Page",
							"revisions": [
								{
									"revid": 42,
									"timestamp": "2024-01-02T03:04:06Z",
									"user": "Bob",
									"comment": "Recovered",
									"content": "World"
								}
							]
						}
					]
				}
			}`
		default:
			t.Fatalf("unexpected query: %q", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	err := importRevids(
		out,
		errOut,
		"origin",
		"http://example.com/api.php",
		nil,
		[]int{41, 42},
		map[string]bool{"Test Page": true, "Recovered Page": true},
		1,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(errOut.String(), "warning: failed to retrieve revision(s): [42]") {
		t.Fatalf("unexpected warning after truncation split: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "truncation detected, splitting batch") {
		t.Fatalf("missing truncation split debug note: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Recovered_Page.mw") {
		t.Fatalf("missing recovered page import after split: %q", out.String())
	}
}

func TestImportRevidsReportsDeletedRevisionTitleInWarning(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case req.URL.Query().Get("prop") == "revisions":
			body = `{
				"query": {
					"pages": [
						{
							"title": "Test Page",
							"revisions": [
								{
									"revid": 41,
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
		case req.URL.Query().Get("prop") == "deletedrevisions":
			body = `{
				"query": {
					"pages": [
						{
							"title": "Deleted Page",
							"deletedrevisions": [
								{
									"revid": 42,
									"timestamp": "2024-02-03T04:05:06Z",
									"user": "Admin",
									"comment": "Removed spam"
								}
							]
						}
					]
				}
			}`
		case req.URL.Query().Get("list") == "logevents":
			body = `{
				"query": {
					"logevents": [
						{
							"user": "Oversighter",
							"timestamp": "2024-02-03T04:06:07Z",
							"comment": "Cleanup",
							"params": {
								"ids": [42]
							}
						}
					]
				}
			}`
		default:
			t.Fatalf("unexpected prop query: %q", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

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
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errOut.String(), `42="Deleted Page" user="Admin" timestamp="2024-02-03T04:05:06Z" comment="Removed spam"`) {
		t.Fatalf("missing deleted revision detail: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), `log="delete/revision by Oversighter at 2024-02-03T04:06:07Z comment=\"Cleanup\""`) {
		t.Fatalf("missing logevent detail: %q", errOut.String())
	}
}

func TestImportRevidsReportsDeletedRevisionPermissionWarning(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case req.URL.Query().Get("prop") == "revisions":
			body = `{
				"query": {
					"pages": [
						{
							"title": "Test Page",
							"revisions": [
								{
									"revid": 41,
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
		case req.URL.Query().Get("prop") == "deletedrevisions":
			body = `{
				"error": {
					"code": "drvpermissiondenied",
					"info": "You don't have permission to view deleted revision information"
				}
			}`
		default:
			t.Fatalf("unexpected prop query: %q", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

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
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errOut.String(), "insufficient permissions to inspect deleted revisions") {
		t.Fatalf("missing permission detail: %q", errOut.String())
	}
}

func TestImportRevidsFailsWhenRequestedRevisionIsMissingOnIncrementalImport(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"query": {
				"pages": [
					{
						"title": "Test Page",
						"revisions": [
							{
								"revid": 41,
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
		2,
	)
	if err == nil {
		t.Fatal("expected missing revision error")
	}
	if !strings.Contains(err.Error(), "failed to retrieve revision(s): [42]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
