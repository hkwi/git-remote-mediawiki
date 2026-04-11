package remotehelper

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"git-remote-mediawiki/client"
)

func TestDoImportUsesRevisionImportByDefault(t *testing.T) {
	oldGit := gitExecWithStdin
	oldLast := getLastGlobalRemoteRevFunc
	oldTracked := collectTrackedPagesFunc
	oldImport := importRevidsFunc
	defer func() {
		gitExecWithStdin = oldGit
		getLastGlobalRemoteRevFunc = oldLast
		collectTrackedPagesFunc = oldTracked
		importRevidsFunc = oldImport
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		return "", "", nil
	}
	getLastGlobalRemoteRevFunc = func(httpClient *http.Client, apiURL string) (int, error) {
		return 3, nil
	}
	collectTrackedPagesFunc = func(httpClient *http.Client, apiURL string, trackedPages, trackedCategories, trackedNamespaces []string) (map[string]bool, error) {
		return map[string]bool{"Test Page": true}, nil
	}

	called := false
	importRevidsFunc = func(w io.Writer, ew io.Writer, remotename, apiURL string, httpClient *http.Client, revisionIDs []int, tracked map[string]bool, fetchFrom int) error {
		called = true
		if fetchFrom != 1 {
			t.Fatalf("unexpected fetchFrom: %d", fetchFrom)
		}
		if len(revisionIDs) != 3 || revisionIDs[0] != 1 || revisionIDs[2] != 3 {
			t.Fatalf("unexpected revisionIDs: %#v", revisionIDs)
		}
		return nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := doImport(out, errOut, "origin", "http://example.com/api.php", "http://example.com", nil); err != nil {
		t.Fatalf("doImport failed: %v", err)
	}
	if !called {
		t.Fatal("expected revision import path to be used")
	}
}

func TestDoImportRevisionImportAddsLinkedMediaForDefaultTracking(t *testing.T) {
	oldGit := gitExecWithStdin
	oldLast := getLastGlobalRemoteRevFunc
	oldTracked := collectTrackedPagesFunc
	oldImport := importRevidsFunc
	oldGetAll := getAllPagesContentFunc
	oldGetAllWithClient := getAllPagesContentWithClientFunc
	defer func() {
		gitExecWithStdin = oldGit
		getLastGlobalRemoteRevFunc = oldLast
		collectTrackedPagesFunc = oldTracked
		importRevidsFunc = oldImport
		getAllPagesContentFunc = oldGetAll
		getAllPagesContentWithClientFunc = oldGetAllWithClient
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" && args[2] == "remote.origin.mediaimport" {
			return "true", "", nil
		}
		return "", "", nil
	}
	getLastGlobalRemoteRevFunc = func(httpClient *http.Client, apiURL string) (int, error) {
		return 1, nil
	}
	collectTrackedPagesFunc = func(httpClient *http.Client, apiURL string, trackedPages, trackedCategories, trackedNamespaces []string) (map[string]bool, error) {
		return map[string]bool{"Test Page": true}, nil
	}
	getAllPagesContentFunc = func(apiURL string, namespace, limit int) ([]client.Page, error) {
		return []client.Page{{PageID: 1, Title: "Test Page", Content: "Link [[File:Foo.txt]]"}}, nil
	}
	getAllPagesContentWithClientFunc = func(httpClient *http.Client, apiURL string, namespace int, limit int) ([]client.Page, error) {
		return []client.Page{{PageID: 1, Title: "Test Page", Content: "Link [[File:Foo.txt]]"}}, nil
	}

	called := false
	importRevidsFunc = func(w io.Writer, ew io.Writer, remotename, apiURL string, httpClient *http.Client, revisionIDs []int, tracked map[string]bool, fetchFrom int) error {
		called = true
		if !tracked["File:Foo.txt"] {
			t.Fatalf("linked media was not added to tracked set: %#v", tracked)
		}
		return nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := doImport(out, errOut, "origin", "http://example.com/api.php", "http://example.com", nil); err != nil {
		t.Fatalf("doImport failed: %v", err)
	}
	if !called {
		t.Fatal("expected revision import path to be used")
	}
}

func TestGetLastGlobalRemoteRevSkipsRecentChangesWithoutRevid(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body: io.NopCloser(bytes.NewBufferString(`{
					"query": {
						"recentchanges": [
							{"type":"log","revid":0},
							{"revid": 42}
						]
					}
				}`)),
				Request: req,
			}, nil
		}),
	}

	got, err := getLastGlobalRemoteRev(client, "http://example.com/api.php")
	if err != nil {
		t.Fatalf("getLastGlobalRemoteRev failed: %v", err)
	}
	if got != 42 {
		t.Fatalf("unexpected revid: got %d want 42", got)
	}
}
