package remotehelper

import (
	"bytes"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestPushCallsEdit(t *testing.T) {
	// Override git and file helpers
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	defer func() { gitExecWithStdin = oldGit }()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		// For rev-parse return a placeholder commit
		if len(args) >= 1 && args[0] == "rev-parse" {
			return "deadbeef", "", nil
		}
		if len(args) >= 4 && args[0] == "log" && args[1] == "--no-walk" && args[2] == "--format=%s" && args[3] == "deadbeef" {
			return "Commit subject\n", "", nil
		}
		return "", "", nil
	}

	listFilesFunc = func(commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	showFileFunc = func(commit, path string) (string, error) {
		return "Hello Push", nil
	}

	var gotTitle, gotContent, gotSummary string
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		gotTitle = title
		gotContent = content
		gotSummary = summary
		if minor {
			t.Fatal("minor should be false")
		}
		return 123, nil
	}
	var gotCommit string
	var gotRevid int64
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		gotCommit = commit
		gotRevid = revid
		return nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}

	if gotTitle != "Test Page" {
		t.Fatalf("unexpected title: %q", gotTitle)
	}
	if gotContent != "Hello Push" {
		t.Fatalf("unexpected content: %q", gotContent)
	}
	if gotSummary != "Commit subject" {
		t.Fatalf("unexpected summary: %q", gotSummary)
	}
	if gotCommit != "deadbeef" || gotRevid != 123 {
		t.Fatalf("unexpected metadata update args: commit=%q revid=%d", gotCommit, gotRevid)
	}
	if !strings.Contains(out.String(), "ok refs/heads/master") {
		t.Fatalf("missing push status: %q", out.String())
	}
}

func TestPushDumbPushSkipsMetadataUpdate(t *testing.T) {
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			return "deadbeef", "", nil
		}
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" && args[2] == "remote.origin.dumbPush" {
			return "true", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) {
		return []string{"Test_Page.mw"}, nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	showFileFunc = func(commit, path string) (string, error) {
		return "Hello Push", nil
	}
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		return 123, nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }

	called := false
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		called = true
		return nil
	}

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}
	if called {
		t.Fatal("metadata update should be skipped for dumbPush")
	}
	if !strings.Contains(errOut.String(), "dumbPush") {
		t.Fatalf("missing dumbPush notice: %q", errOut.String())
	}
}

func TestPushPropagatesDeletedPage(t *testing.T) {
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			if len(args) >= 2 && args[1] == "refs/mediawiki/origin/master^0" {
				return "basebeef", "", nil
			}
			return "deadbeef", "", nil
		}
		if len(args) >= 4 && args[0] == "log" && args[1] == "--no-walk" && args[2] == "--format=%s" && args[3] == "deadbeef" {
			return "Delete subject\n", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) {
		return nil, nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }
	showFileFunc = func(commit, path string) (string, error) {
		return "", nil
	}
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		t.Fatalf("editPage should not be called")
		return 0, nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) {
		if base != "basebeef" || commit != "deadbeef" {
			t.Fatalf("unexpected diff args: base=%q commit=%q", base, commit)
		}
		return []string{"Foo.mw"}, nil
	}

	var gotTitle, gotReason string
	deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
		gotTitle = title
		gotReason = reason
		return 456, nil
	}

	var gotCommit string
	var gotRevid int64
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		gotCommit = commit
		gotRevid = revid
		return nil
	}

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}

	if gotTitle != "Foo" {
		t.Fatalf("unexpected deleted title: %q", gotTitle)
	}
	if gotReason != "Delete subject" {
		t.Fatalf("unexpected delete reason: %q", gotReason)
	}
	if gotCommit != "deadbeef" || gotRevid != 456 {
		t.Fatalf("unexpected metadata update args: commit=%q revid=%d", gotCommit, gotRevid)
	}
	if !strings.Contains(out.String(), "ok refs/heads/master") {
		t.Fatalf("missing push status: %q", out.String())
	}
}

func TestPushReportsErrorWhenAnyPageEditFails(t *testing.T) {
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			return "deadbeef", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) {
		return []string{"Good.mw", "Bad.mw"}, nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) {
		return []string{"Good.mw", "Bad.mw"}, nil
	}
	showFileFunc = func(commit, path string) (string, error) {
		return "Hello Push", nil
	}
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		if title == "Bad" {
			return 0, errors.New("permission denied")
		}
		return 123, nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }

	called := false
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		called = true
		return nil
	}

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}
	if called {
		t.Fatal("metadata should not be updated after a partial push failure")
	}
	if !strings.Contains(out.String(), "error refs/heads/master push failed") {
		t.Fatalf("missing push error status: stdout=%q stderr=%q", out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "ok refs/heads/master") {
		t.Fatalf("push should not report ok after a partial failure: %q", out.String())
	}
}

func TestPushOnlySendsChangedFiles(t *testing.T) {
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			if len(args) >= 2 && args[1] == "refs/mediawiki/origin/master^0" {
				return "basebeef", "", nil
			}
			return "deadbeef", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) {
		return []string{"Unchanged.mw", "Changed.mw"}, nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) {
		if base != "basebeef" || commit != "deadbeef" {
			t.Fatalf("unexpected changed diff args: base=%q commit=%q", base, commit)
		}
		return []string{"Changed.mw"}, nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }

	var shown []string
	showFileFunc = func(commit, path string) (string, error) {
		shown = append(shown, path)
		return "content", nil
	}
	var edited []string
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		edited = append(edited, title)
		return 111, nil
	}
	deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
		t.Fatalf("deletePage should not be called")
		return 0, nil
	}
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error { return nil }

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}

	if len(shown) != 1 || shown[0] != "Changed.mw" {
		t.Fatalf("unexpected shown files: %#v", shown)
	}
	if len(edited) != 1 || edited[0] != "Changed" {
		t.Fatalf("unexpected edited titles: %#v", edited)
	}
}

func TestPushUsesMinorFlagFromNotes(t *testing.T) {
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			return "deadbeef", "", nil
		}
		if len(args) >= 4 && args[0] == "notes" && args[1] == "--ref=mediawiki-options" && args[2] == "show" && args[3] == "deadbeef" {
			return "minor: true\n", "", nil
		}
		return "", "", nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }
	showFileFunc = func(commit, path string) (string, error) { return "Hello Push", nil }

	called := false
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		called = true
		if !minor {
			t.Fatal("expected minor=true")
		}
		return 123, nil
	}
	deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
		t.Fatalf("deletePage should not be called")
		return 0, nil
	}
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error { return nil }

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}
	if !called {
		t.Fatal("expected editPage to be called")
	}
}

func TestListFilesParsesNULSeparatedUTF8Paths(t *testing.T) {
	oldGit := gitExec
	defer func() { gitExec = oldGit }()

	gitExec = func(args ...string) (string, string, error) {
		want := []string{"ls-tree", "-r", "-z", "--name-only", "deadbeef"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("unexpected git args: %#v", args)
		}
		return "日本語ページ.mw\x00 spaced page.mw\x00image.png\x00", "", nil
	}

	got, err := listFilesFunc("deadbeef")
	if err != nil {
		t.Fatalf("listFilesFunc failed: %v", err)
	}
	want := []string{"日本語ページ.mw", " spaced page.mw", "image.png"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected files: got %#v want %#v", got, want)
	}
}

func TestNameStatusHelpersParseNULSeparatedUTF8Paths(t *testing.T) {
	oldGit := gitExec
	defer func() { gitExec = oldGit }()

	diffOutput := strings.Join([]string{
		"M", "システムリスク評価チェックリスト%2FDGS00164.mw",
		"A", "追加ページ.mw",
		"D", "削除ページ.mw",
		"R100", "旧ページ.mw", "新ページ.mw",
		"A", "upload.png",
		"D", "old.bin",
		"R100", "old.dat", "new.dat",
		"",
	}, "\x00")

	gitExec = func(args ...string) (string, string, error) {
		want := []string{"diff", "--name-status", "-z", "--find-renames", "basebeef", "deadbeef"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("unexpected git args: %#v", args)
		}
		return diffOutput, "", nil
	}

	changedMW, err := changedMWFilesFunc("basebeef", "deadbeef")
	if err != nil {
		t.Fatalf("changedMWFilesFunc failed: %v", err)
	}
	wantChangedMW := []string{
		"システムリスク評価チェックリスト%2FDGS00164.mw",
		"追加ページ.mw",
		"新ページ.mw",
	}
	if !reflect.DeepEqual(changedMW, wantChangedMW) {
		t.Fatalf("unexpected changed mw files: got %#v want %#v", changedMW, wantChangedMW)
	}

	deletedMW, err := deletedMWFilesFunc("basebeef", "deadbeef")
	if err != nil {
		t.Fatalf("deletedMWFilesFunc failed: %v", err)
	}
	wantDeletedMW := []string{"削除ページ.mw", "旧ページ.mw"}
	if !reflect.DeepEqual(deletedMW, wantDeletedMW) {
		t.Fatalf("unexpected deleted mw files: got %#v want %#v", deletedMW, wantDeletedMW)
	}

	changedMedia, err := changedMediaFilesFunc("basebeef", "deadbeef")
	if err != nil {
		t.Fatalf("changedMediaFilesFunc failed: %v", err)
	}
	wantChangedMedia := []string{"upload.png", "new.dat"}
	if !reflect.DeepEqual(changedMedia, wantChangedMedia) {
		t.Fatalf("unexpected changed media files: got %#v want %#v", changedMedia, wantChangedMedia)
	}

	deletedMedia, err := deletedMediaFilesFunc("basebeef", "deadbeef")
	if err != nil {
		t.Fatalf("deletedMediaFilesFunc failed: %v", err)
	}
	wantDeletedMedia := []string{"old.bin", "old.dat"}
	if !reflect.DeepEqual(deletedMedia, wantDeletedMedia) {
		t.Fatalf("unexpected deleted media files: got %#v want %#v", deletedMedia, wantDeletedMedia)
	}
}
