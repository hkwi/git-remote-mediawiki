package remotehelper

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"git-remote-mediawiki/client"
)

func TestDoImportShallowMediaImportIncludesLinkedFile(t *testing.T) {
	oldGetAll := getAllPagesContentFunc
	oldGetTitles := getPagesByTitlesWithClientFunc
	oldDownload := downloadFileFunc
	oldGit := gitExecWithStdin
	defer func() {
		getAllPagesContentFunc = oldGetAll
		getPagesByTitlesWithClientFunc = oldGetTitles
		downloadFileFunc = oldDownload
		gitExecWithStdin = oldGit
	}()

	getAllPagesContentFunc = func(apiURL string, namespace, limit int) ([]client.Page, error) {
		return []client.Page{{PageID: 1, Title: "Testpage", Content: "I am linking [[File:File.txt]]"}}, nil
	}
	getPagesByTitlesWithClientFunc = func(httpClient *http.Client, apiURL string, titles []string) ([]client.Page, error) {
		if len(titles) != 1 || titles[0] != "File:File.txt" {
			t.Fatalf("unexpected linked media titles: %#v", titles)
		}
		return []client.Page{{PageID: 2, Title: "File:File.txt", Content: "file page"}}, nil
	}
	downloadFileFunc = func(apiURL string, httpClient *http.Client, title string) ([]byte, error) {
		if title != "File:File.txt" {
			t.Fatalf("unexpected download title: %q", title)
		}
		return []byte("binary-data"), nil
	}
	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" {
			switch args[2] {
			case "remote.origin.shallow", "remote.origin.mediaimport":
				return "true", "", nil
			}
		}
		return "", "", nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := doImport(out, errOut, "origin", "http://example.com/api.php", "http://example.com", nil); err != nil {
		t.Fatalf("doImport failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "\"File:File.txt.mw\"") {
		t.Fatalf("missing file page import: %q", got)
	}
	if !strings.Contains(got, "\"File.txt\"") {
		t.Fatalf("missing binary file import: %q", got)
	}
}

func TestPushMediaExportUploadsChangedFiles(t *testing.T) {
	oldList := listFilesFunc
	oldShowBytes := showFileBytesFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldUpload := uploadFile
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	oldDeletedMedia := deletedMediaFilesFunc
	oldChangedMedia := changedMediaFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileBytesFunc = oldShowBytes
		editPage = oldEdit
		deletePage = oldDelete
		uploadFile = oldUpload
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
		deletedMediaFilesFunc = oldDeletedMedia
		changedMediaFilesFunc = oldChangedMedia
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			if len(args) >= 2 && args[1] == "refs/mediawiki/origin/master^0" {
				return "basebeef", "", nil
			}
			return "deadbeef", "", nil
		}
		if len(args) >= 4 && args[0] == "log" && args[1] == "--no-walk" && args[2] == "--format=%s" && args[3] == "deadbeef" {
			return "Upload subject\n", "", nil
		}
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" && args[2] == "remote.origin.mediaexport" {
			return "true", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) { return nil, nil }
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }
	changedMediaFilesFunc = func(base, commit string) ([]string, error) {
		return []string{"Foo.txt"}, nil
	}
	deletedMediaFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }
	showFileBytesFunc = func(commit, path string) ([]byte, error) {
		if path != "Foo.txt" {
			t.Fatalf("unexpected media path: %q", path)
		}
		return []byte("hello world"), nil
	}
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		t.Fatalf("editPage should not be called")
		return 0, nil
	}
	deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
		t.Fatalf("deletePage should not be called")
		return 0, nil
	}
	var uploaded string
	uploadFile = func(httpClient *http.Client, apiURL, filename string, content []byte, comment string, minor bool) (int64, error) {
		uploaded = filename
		if string(content) != "hello world" {
			t.Fatalf("unexpected upload content: %q", string(content))
		}
		if comment != "Upload subject" {
			t.Fatalf("unexpected upload comment: %q", comment)
		}
		if minor {
			t.Fatal("minor should be false")
		}
		return 999, nil
	}
	var gotRevid int64
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		gotRevid = revid
		return nil
	}

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}
	if uploaded != "Foo.txt" {
		t.Fatalf("unexpected uploaded file: %q", uploaded)
	}
	if gotRevid != 999 {
		t.Fatalf("unexpected metadata revision: %d", gotRevid)
	}
}

func TestImportRevidsMediaImportUsesRevisionTimestamp(t *testing.T) {
	oldDownload := downloadFileAtTimestampFunc
	defer func() { downloadFileAtTimestampFunc = oldDownload }()

	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"query": {
				"pages": [
					{
						"title": "File:File.txt",
						"revisions": [
							{
								"revid": 42,
								"timestamp": "2024-01-02T03:04:05Z",
								"user": "Alice",
								"comment": "Imported file page",
								"content": "file page body"
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

	var gotTitle, gotTimestamp string
	downloadFileAtTimestampFunc = func(apiURL string, httpClient *http.Client, title, timestamp string) ([]byte, error) {
		gotTitle = title
		gotTimestamp = timestamp
		return []byte("historic-binary"), nil
	}

	oldGit := gitExecWithStdin
	defer func() { gitExecWithStdin = oldGit }()
	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" && args[2] == "remote.origin.mediaimport" {
			return "true", "", nil
		}
		return "", "", nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	err := importRevids(out, errOut, "origin", "http://example.com/api.php", nil, []int{42}, map[string]bool{"File:File.txt": true}, 1)
	if err != nil {
		t.Fatalf("importRevids failed: %v", err)
	}
	if gotTitle != "File:File.txt" || gotTimestamp != "2024-01-02T03:04:05Z" {
		t.Fatalf("unexpected media fetch args: title=%q timestamp=%q", gotTitle, gotTimestamp)
	}
	if !strings.Contains(out.String(), "\"File.txt\"") {
		t.Fatalf("missing binary file import: %q", out.String())
	}
}
