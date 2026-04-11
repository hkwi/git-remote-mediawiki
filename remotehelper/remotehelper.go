package remotehelper

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"git-remote-mediawiki/client"
	"net/http"
)

// getAllPagesContentFunc can be overridden in tests.
var getAllPagesContentFunc = func(apiURL string, namespace, limit int) ([]client.Page, error) {
	return client.GetAllPagesContent(apiURL, namespace, limit)
}

var getLastGlobalRemoteRevFunc = func(httpClient *http.Client, apiURL string) (int, error) {
	return getLastGlobalRemoteRev(httpClient, apiURL)
}

// ErrAuthFailed is returned when login to the wiki fails and the helper
// should terminate rather than continue.
var ErrAuthFailed = errors.New("authentication failed")

var getPagesByTitlesWithClientFunc = func(httpClient *http.Client, apiURL string, titles []string) ([]client.Page, error) {
	return client.GetPagesByTitlesWithClient(httpClient, apiURL, titles)
}

var getAllPagesContentWithClientFunc = func(httpClient *http.Client, apiURL string, namespace int, limit int) ([]client.Page, error) {
	return client.GetAllPagesContentWithClient(httpClient, apiURL, namespace, limit)
}

var collectTrackedPagesFunc = func(httpClient *http.Client, apiURL string, trackedPages, trackedCategories, trackedNamespaces []string) (map[string]bool, error) {
	return collectTrackedPages(httpClient, apiURL, trackedPages, trackedCategories, trackedNamespaces)
}

var importRevidsFunc = func(w io.Writer, ew io.Writer, remotename, apiURL string, httpClient *http.Client, revisionIDs []int, tracked map[string]bool, fetchFrom int) error {
	return importRevids(w, ew, remotename, apiURL, httpClient, revisionIDs, tracked, fetchFrom)
}

var downloadFileFunc = func(apiURL string, httpClient *http.Client, title string) ([]byte, error) {
	return client.DownloadFile(apiURL, httpClient, title)
}

var downloadFileAtTimestampFunc = func(apiURL string, httpClient *http.Client, title, timestamp string) ([]byte, error) {
	return client.DownloadFileAtTimestamp(apiURL, httpClient, title, timestamp)
}

var updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
	if _, _, err := gitExec("update-ref", "refs/mediawiki/"+remotename+"/master", commit); err != nil {
		return err
	}
	if revid <= 0 {
		return nil
	}
	note := fmt.Sprintf("mediawiki_revision: %d", revid)
	if _, _, err := gitExec("notes", "--ref=commits", "add", "-f", "-m", note, commit); err != nil {
		return err
	}
	if _, _, err := gitExec("notes", "--ref="+remotename+"/mediawiki", "add", "-f", "-m", note, commit); err != nil {
		return err
	}
	return nil
}

var progressEnabled bool

func emitProgress(w io.Writer, format string, args ...interface{}) {
	if !progressEnabled || w == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(w, "progress %s\n", msg)
}

func debugEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("GIT_REMOTE_MEDIAWIKI_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// gitExecWithStdinImpl runs `git` with the provided stdin and returns
// stdout, stderr and any execution error. This is the underlying
// implementation used by the default `gitExec`.
func gitExecWithStdinImpl(stdin string, args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	outBytes, err := cmd.Output()
	outStr := string(outBytes)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return outStr, string(ee.Stderr), err
		}
		return outStr, "", err
	}
	return outStr, "", nil
}

// defaultGitExec is the default implementation used for `gitExec`.
var defaultGitExec = func(args ...string) (string, string, error) {
	return gitExecWithStdin("", args...)
}

// gitExec is the central helper used by production code to invoke `git`.
// Tests may override this variable to mock git outputs. The default
// implementation runs `git` and returns stdout, stderr, and error.
var gitExec = defaultGitExec

// gitExecWithStdin runs `git` with the provided stdin. Tests may
// override this variable to mock git invocations that need stdin.
var gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
	return gitExecWithStdinImpl(stdin, args...)
}

// debugf writes a debug message to the provided error writer with a DEBUG prefix.
func debugf(ew io.Writer, format string, args ...interface{}) {
	if !debugEnabled() {
		return
	}
	if ew == nil {
		ew = os.Stderr
	}
	fmt.Fprintf(ew, "DEBUG: "+format+"\n", args...)
}

// NOTE: avoid masking assembled strings after the fact. When logging
// credential input we build a separate masked representation at
// assembly time so raw secrets never appear in logs.

// getCredentialsFromGitCredential asks git credential helper to fill credentials
// for the given remote URL. It sends url/protocol/host/path and optional
// username as input and parses username/password from the helper output.
func getCredentialsFromGitCredential(ew io.Writer, remoteURL, usernameCfg string) (string, string, error) {
	u, err := url.Parse(remoteURL)
	if err != nil {
		// fall back to using the raw URL
		u = &url.URL{Scheme: "", Host: remoteURL, Path: "/"}
	}
	// Build input for `git credential fill` (do not log secrets here).
	in := fmt.Sprintf("url=%s\n", remoteURL)
	if u.Scheme != "" {
		in += fmt.Sprintf("protocol=%s\n", u.Scheme)
	}
	if u.Host != "" {
		in += fmt.Sprintf("host=%s\n", u.Host)
	}
	if u.Path != "" {
		in += fmt.Sprintf("path=%s\n", u.Path)
	}
	if usernameCfg != "" {
		in += fmt.Sprintf("username=%s\n", usernameCfg)
	}
	// Intentionally do not send any existing password to the credential helper.
	in += "\n"

	// Prefer calling through gitExecWithStdin so tests can mock credential helper
	// responses. The provided stdin is used by the default implementation.
	outStr, errOut, err := gitExecWithStdin(in, "credential", "fill")
	if err != nil {
		return usernameCfg, "", err
	}
	// Parse only stdout for credential key/value pairs.
	// Treat stderr as diagnostics only and do not include it in parsing
	// to avoid accidentally capturing sensitive data that may be logged
	// by helpers when tracing/debugging is enabled.
	if strings.TrimSpace(errOut) != "" {
		debugf(ew, "git credential helper stderr: %s", errOut)
	}
	var user, pass string
	for _, line := range strings.Split(outStr, "\n") {
		if strings.HasPrefix(line, "username=") {
			user = strings.TrimPrefix(line, "username=")
		} else if strings.HasPrefix(line, "password=") {
			pass = strings.TrimPrefix(line, "password=")
		}
	}
	if user == "" {
		user = usernameCfg
	}
	return user, pass, nil
}

// sendCredentialsToGitCredential sends the given username and url to
// the git credential helper with the specified action (approve/reject).
// Passwords are intentionally not passed to the helper.
func sendCredentialsToGitCredential(ew io.Writer, action, remoteURL, username string) error {
	in := fmt.Sprintf("url=%s\n", remoteURL)
	if username != "" {
		in += fmt.Sprintf("username=%s\n", username)
	}
	// Intentionally do not send any password to the credential helper.
	in += "\n"

	// Prefer calling through gitExecWithStdin so tests can mock and avoid spawning
	// a real git credential helper. The provided stdin is used by the default implementation.
	outStr, errOut, err := gitExecWithStdin(in, "credential", action)
	if err != nil {
		return err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(ew, "git credential helper stderr: %s", errOut)
	}
	hasPass := false
	for _, line := range strings.Split(outStr, "\n") {
		if strings.HasPrefix(line, "password=") {
			hasPass = true
			break
		}
	}
	debugf(ew, "git credential %s output: password_present=%t", action, hasPass)
	return nil
}

// listFilesFunc lists files in a commit (ls-tree -r --name-only). Override in tests.
var listFilesFunc = func(commit string) ([]string, error) {
	out, errOut, err := gitExec("ls-tree", "-r", "--name-only", commit)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(nil, "git ls-tree stderr: %s", errOut)
	}
	// Keep stdout intact overall; trim per-line when adding to files to
	// normalize surrounding whitespace but preserve internal spaces.
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		files = append(files, strings.TrimSpace(line))
	}
	return files, nil
}

// showFileFunc returns the blob content for a given commit:path (git show).
var showFileFunc = func(commit, path string) (string, error) {
	out, errOut, err := gitExec("show", fmt.Sprintf("%s:%s", commit, path))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(nil, "git show stderr: %s", errOut)
	}
	// Return stdout as-is; do not trim content which may contain significant
	// leading/trailing whitespace. stderr is logged above.
	return out, nil
}

var showFileBytesFunc = func(commit, path string) ([]byte, error) {
	out, errOut, err := gitExec("show", fmt.Sprintf("%s:%s", commit, path))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(nil, "git show stderr: %s", errOut)
	}
	return []byte(out), nil
}

var deletedMWFilesFunc = func(base, commit string) ([]string, error) {
	if strings.TrimSpace(base) == "" {
		return nil, nil
	}
	out, errOut, err := gitExec("diff", "--name-status", "--find-renames", base, commit)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(nil, "git diff --name-status stderr: %s", errOut)
	}
	// Keep stdout intact; we'll skip empty/whitespace-only lines below.
	var deleted []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		switch {
		case status == "D":
			if strings.HasSuffix(fields[1], ".mw") {
				deleted = append(deleted, fields[1])
			}
		case strings.HasPrefix(status, "R"):
			if strings.HasSuffix(fields[1], ".mw") {
				deleted = append(deleted, fields[1])
			}
		}
	}
	return deleted, nil
}

var changedMWFilesFunc = func(base, commit string) ([]string, error) {
	if strings.TrimSpace(base) == "" {
		return listFilesFunc(commit)
	}
	out, errOut, err := gitExec("diff", "--name-status", "--find-renames", base, commit)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(nil, "git diff --name-status stderr: %s", errOut)
	}
	// Keep stdout intact; we'll skip empty/whitespace-only lines below.
	seen := make(map[string]bool)
	var changed []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		switch {
		case status == "A" || status == "M":
			if strings.HasSuffix(fields[1], ".mw") && !seen[fields[1]] {
				seen[fields[1]] = true
				changed = append(changed, fields[1])
			}
		case strings.HasPrefix(status, "R"):
			if len(fields) >= 3 && strings.HasSuffix(fields[2], ".mw") && !seen[fields[2]] {
				seen[fields[2]] = true
				changed = append(changed, fields[2])
			}
		}
	}
	return changed, nil
}

var deletedMediaFilesFunc = func(base, commit string) ([]string, error) {
	if strings.TrimSpace(base) == "" {
		return nil, nil
	}
	out, errOut, err := gitExec("diff", "--name-status", "--find-renames", base, commit)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(nil, "git diff --name-status stderr: %s", errOut)
	}
	// Keep stdout intact; we'll skip empty/whitespace-only lines below.
	var deleted []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		switch {
		case status == "D":
			if !strings.HasSuffix(fields[1], ".mw") {
				deleted = append(deleted, fields[1])
			}
		case strings.HasPrefix(status, "R"):
			if !strings.HasSuffix(fields[1], ".mw") {
				deleted = append(deleted, fields[1])
			}
		}
	}
	return deleted, nil
}

var changedMediaFilesFunc = func(base, commit string) ([]string, error) {
	if strings.TrimSpace(base) == "" {
		files, err := listFilesFunc(commit)
		if err != nil {
			return nil, err
		}
		var changed []string
		for _, f := range files {
			if !strings.HasSuffix(f, ".mw") {
				changed = append(changed, f)
			}
		}
		return changed, nil
	}
	out, errOut, err := gitExec("diff", "--name-status", "--find-renames", base, commit)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(errOut) != "" {
		debugf(nil, "git diff --name-status stderr: %s", errOut)
	}
	// Keep stdout intact; we'll skip empty/whitespace-only lines below.
	seen := make(map[string]bool)
	var changed []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		switch {
		case status == "A" || status == "M":
			if !strings.HasSuffix(fields[1], ".mw") && !seen[fields[1]] {
				seen[fields[1]] = true
				changed = append(changed, fields[1])
			}
		case strings.HasPrefix(status, "R"):
			if len(fields) >= 3 && !strings.HasSuffix(fields[2], ".mw") && !seen[fields[2]] {
				seen[fields[2]] = true
				changed = append(changed, fields[2])
			}
		}
	}
	return changed, nil
}

// editPage performs the actual page edit; overrideable for tests. The httpClient
// may be nil to use defaults or an authenticated client.
var editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
	return client.EditPage(apiURL, httpClient, title, content, summary, minor)
}

var deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
	return client.DeletePage(apiURL, httpClient, title, reason)
}

var uploadFile = func(httpClient *http.Client, apiURL, filename string, content []byte, comment string, minor bool) (int64, error) {
	return client.UploadFile(apiURL, httpClient, filename, content, comment, minor)
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: git-remote-mediawiki <remote-name> <url>")
}

func Run(r io.Reader, w io.Writer, ew io.Writer, remotename, url string) error {
	reader := bufio.NewReader(r)

	// normalize api url
	apiURL := normalizeAPIURL(url)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		cmd := parts[0]
		var arg string
		if len(parts) > 1 {
			arg = parts[1]
		}

		switch cmd {
		case "capabilities":
			fmt.Fprintln(w, "refspec refs/heads/*:refs/mediawiki/"+remotename+"/*")
			fmt.Fprintln(w, "import")
			fmt.Fprintln(w, "list")
			fmt.Fprintln(w, "option")
			fmt.Fprintln(w, "push")
			fmt.Fprintln(w)
		case "list":
			fmt.Fprintln(w, "? refs/heads/master")
			fmt.Fprintln(w, "@refs/heads/master HEAD")
			fmt.Fprintln(w)
		case "option":
			optParts := strings.SplitN(arg, " ", 2)
			if len(optParts) != 2 {
				fmt.Fprintln(w, "unsupported")
				continue
			}
			switch optParts[0] {
			case "progress":
				progressEnabled = strings.EqualFold(strings.TrimSpace(optParts[1]), "true")
				fmt.Fprintln(w, "ok")
			case "verbosity":
				fmt.Fprintln(w, "ok")
			default:
				fmt.Fprintln(w, "unsupported")
			}
		case "import":
			// Collect subsequent import lines until blank line
			refs := []string{arg}
			for {
				// peek next line
				pos, _ := reader.Peek(1)
				if len(pos) == 0 {
					break
				}
				next, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				next = strings.TrimRight(next, "\r\n")
				if next == "" {
					break
				}
				if strings.HasPrefix(next, "import ") {
					refs = append(refs, strings.TrimSpace(strings.TrimPrefix(next, "import ")))
				} else {
					// unexpected, ignore
				}
			}
			// perform import
			if err := doImport(w, ew, remotename, apiURL, url, refs); err != nil {
				if errors.Is(err, ErrAuthFailed) {
					return err
				}
				fmt.Fprintf(ew, "import failed: %v\n", err)
			} else {
				fmt.Fprintln(w, "done")
			}
		case "push":
			// Collect subsequent push lines until blank line
			refs := []string{arg}
			for {
				pos, _ := reader.Peek(1)
				if len(pos) == 0 {
					break
				}
				next, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				next = strings.TrimRight(next, "\r\n")
				if next == "" {
					break
				}
				if strings.HasPrefix(next, "push ") {
					refs = append(refs, strings.TrimSpace(strings.TrimPrefix(next, "push ")))
				}
			}
			if err := doPush(w, ew, remotename, apiURL, url, refs); err != nil {
				if errors.Is(err, ErrAuthFailed) {
					return err
				}
				fmt.Fprintf(ew, "push failed: %v\n", err)
			}
		default:
			fmt.Fprintf(ew, "Unknown command: %s\n", line)
			return nil
		}
	}
}

func normalizeAPIURL(raw string) string {
	s := raw
	if strings.HasPrefix(s, "mediawiki://") {
		s = "http://" + strings.TrimPrefix(s, "mediawiki://")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	if strings.HasSuffix(s, "/api.php") {
		return s
	}
	s = strings.TrimRight(s, "/")
	return s + "/api.php"
}

func escapePath(p string) string {
	// Escape backslash, double-quote and newlines similar to fe_escape_path
	p = strings.ReplaceAll(p, "\\", "\\\\")
	p = strings.ReplaceAll(p, "\"", "\\\"")
	p = strings.ReplaceAll(p, "\n", "\\n")
	return fmt.Sprintf("\"%s\"", p)
}

func getShallow(remotename string) bool {
	return getBoolConfig(remotename, "shallow")
}

func getDumbPush(remotename string) bool {
	return getBoolConfig(remotename, "dumbPush")
}

func getMediaImport(remotename string) bool {
	// Allow forcing via environment for test scenarios where -c does not
	// propagate into the helper. Honor common truthy values.
	if v := os.Getenv("GIT_REMOTE_MEDIAWIKI_MEDIAIMPORT"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	}
	return getBoolConfig(remotename, "mediaimport")
}

func getMediaExport(remotename string) bool {
	return getBoolConfig(remotename, "mediaexport")
}

func refExists(ref string) bool {
	_, _, err := gitExec("rev-parse", "--verify", ref)
	return err == nil
}

// getLastLocalRevision reads the git note for the last imported mediawiki revision.
// Returns 0 if none found.
func getLastLocalRevision(remotename string) (int, error) {
	refTarget := "refs/mediawiki/" + remotename + "/master"
	candidates := [][]string{
		{"notes", "--ref=" + remotename + "/mediawiki", "show", refTarget},
		{"notes", "--ref=commits", "show", refTarget},
		{"notes", "show", refTarget},
	}
	for _, args := range candidates {
		out, errOut, err := gitExec(args...)
		if err != nil {
			continue
		}
		if strings.TrimSpace(errOut) != "" {
			debugf(nil, "git notes show stderr: %s", errOut)
		}
		out = strings.TrimSpace(out)
		fields := strings.Fields(out)
		if len(fields) >= 2 && fields[0] == "mediawiki_revision:" {
			if n, err := strconv.Atoi(strings.TrimSpace(fields[1])); err == nil {
				return n, nil
			}
		}
	}
	return 0, nil
}

func getCommitMinor(commit string) bool {
	candidates := [][]string{
		{"notes", "--ref=mediawiki-options", "show", commit},
		{"notes", "--ref=refs/notes/mediawiki-options", "show", commit},
	}
	for _, args := range candidates {
		out, errOut, err := gitExec(args...)
		if err != nil {
			continue
		}
		if strings.TrimSpace(errOut) != "" {
			debugf(nil, "git notes show stderr: %s", errOut)
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(strings.ToLower(line))
			switch line {
			case "minor", "minor: true", "minor=true", "mediawiki_minor: true", "mediawiki_minor=true":
				return true
			}
		}
	}
	return false
}

// getLastGlobalRemoteRev queries the wiki for the most recent revision id.
func getLastGlobalRemoteRev(httpClient *http.Client, apiURL string) (int, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "recentchanges")
	params.Set("rclimit", "50")
	params.Set("rcdir", "older")
	params.Set("rcprop", "ids")
	params.Set("format", "json")
	reqURL := apiURL + "?" + params.Encode()
	resp, err := client.DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req, nil
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var data map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return 0, err
	}
	if q, ok := data["query"].(map[string]interface{}); ok {
		if rc, ok := q["recentchanges"].([]interface{}); ok && len(rc) > 0 {
			for _, item := range rc {
				entry, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				if rn, ok := entry["revid"].(json.Number); ok {
					if n64, err := rn.Int64(); err == nil && n64 > 0 {
						return int(n64), nil
					}
				} else if rf, ok := entry["revid"].(float64); ok && rf > 0 {
					return int(rf), nil
				}
			}
		}
	}
	return 0, fmt.Errorf("could not determine last remote revision")
}

// collectTrackedPages builds a set of tracked page titles according to
// remote.<name>.pages/categories/namespaces (uses API when needed).
func collectTrackedPages(httpClient *http.Client, apiURL string, trackedPages, trackedCategories, trackedNamespaces []string) (map[string]bool, error) {
	pages := make(map[string]bool)
	// explicit titles
	for _, t := range trackedPages {
		pages[t] = true
	}
	// categories
	for _, cat := range trackedCategories {
		cms, err := client.GetCategoryMembersWithClient(httpClient, apiURL, cat)
		if err != nil {
			return nil, err
		}
		for _, t := range cms {
			pages[t] = true
		}
	}
	// namespaces
	for _, ns := range trackedNamespaces {
		nsName := strings.ReplaceAll(ns, "_", " ")
		var nsid int
		if n, err := strconv.Atoi(nsName); err == nil {
			nsid = n
		} else if nsName == "(Main)" {
			nsid = 0
		} else {
			nid, err := client.GetNamespaceIDWithClient(httpClient, apiURL, nsName)
			if err != nil {
				return nil, err
			}
			nsid = nid
		}
		pgs, err := getAllPagesContentWithClientFunc(httpClient, apiURL, nsid, 0)
		if err != nil {
			return nil, err
		}
		for _, p := range pgs {
			pages[p.Title] = true
		}
	}
	// if nothing specified, include all pages
	if len(pages) == 0 {
		all, err := getAllPagesContentWithClientFunc(httpClient, apiURL, 0, 0)
		if err != nil {
			return nil, err
		}
		for _, p := range all {
			pages[p.Title] = true
		}
	}
	return pages, nil
}

// parseMediaWikiTimestamp attempts to parse MediaWiki timestamp into unix epoch
func parseMediaWikiTimestamp(ts string) int64 {
	if ts == "" {
		return time.Now().Unix()
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Unix()
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z07:00", ts); err == nil {
		return t.Unix()
	}
	return time.Now().Unix()
}

func sanitizeIdentName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r':
			return ' '
		case r == '<' || r == '>':
			return -1
		case r < 0x20:
			return -1
		default:
			return r
		}
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "git-mediawiki"
	}
	return s
}

func sanitizeEmailLocal(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	out := ""
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out += string(r)
		case r == '.' || r == '_' || r == '-' || r == '+':
			out += string(r)
		default:
			out += "_"
		}
	}
	out = strings.Trim(out, "._-+")
	if out == "" {
		return "git-mediawiki"
	}
	return out
}

// writeData writes a fast-import "data N" block. It ensures the provided
// string ends with a single terminating newline, writes the correct byte
// length and then the raw bytes. This matches the behavior of the Perl
// reference implementation which includes the trailing newline in the
// advertised length.
func writeData(w io.Writer, s string) {
	// normalize to exactly one trailing newline to avoid producing
	// runs of 3+ newlines when content already has trailing blank lines.
	b := []byte(s)
	// trim any trailing newlines
	for len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	// append single newline
	b = append(b, '\n')
	fmt.Fprintf(w, "data %d\n", len(b))
	_, _ = w.Write(b)
}

func writeDataBytes(w io.Writer, b []byte) {
	fmt.Fprintf(w, "data %d\n", len(b))
	_, _ = w.Write(b)
}

var fileLinkPattern = regexp.MustCompile(`\[\[(?:File|Image):([^|\]]+)`)

func extractFileTitles(content string) []string {
	matches := fileLinkPattern.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var titles []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		title := strings.TrimSpace(m[1])
		if title == "" {
			continue
		}
		full := "File:" + strings.ReplaceAll(title, "_", " ")
		if !seen[full] {
			seen[full] = true
			titles = append(titles, full)
		}
	}
	return titles
}

func getBoolConfig(remotename, key string) bool {
	// First, try to read from git config normally.
	v, _, _ := gitExec("config", "--get", "remote."+remotename+"."+key)
	// If not found, also check GIT_CONFIG_PARAMETERS which contains -c overrides
	if strings.TrimSpace(v) == "" {
		// parse GIT_CONFIG_PARAMETERS env var for '-c key=value' entries
		if params := os.Getenv("GIT_CONFIG_PARAMETERS"); params != "" {
			for _, part := range strings.Split(params, "-c") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				// token may include other -c entries after a space; take first token
				token := part
				if i := strings.IndexAny(part, " \t\n"); i >= 0 {
					token = part[:i]
				}
				if strings.HasPrefix(token, "remote."+remotename+"."+key+"=") {
					v = strings.TrimPrefix(token, "remote."+remotename+"."+key+"=")
					break
				}
				if strings.HasPrefix(token, "mediawiki."+key+"=") {
					v = strings.TrimPrefix(token, "mediawiki."+key+"=")
					break
				}
			}
		}
		if strings.TrimSpace(v) == "" {
			v, _, _ = gitExec("config", "--get", "mediawiki."+key)
		}
	}
	debugf(nil, "getBoolConfig remote.%s.%s -> %q", remotename, key, v)
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// importRevids imports revisions by id, creating one commit per revision for tracked pages.
func importRevids(w io.Writer, ew io.Writer, remotename, apiURL string, httpClient *http.Client, revisionIDs []int, tracked map[string]bool, fetchFrom int) error {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	fullImport := fetchFrom == 1
	mediaImport := getMediaImport(remotename)
	debugf(ew, "importRevids start: remotename=%s mediaImport=%t revCount=%d tracked=%d fetchFrom=%d", remotename, mediaImport, len(revisionIDs), len(tracked), fetchFrom)
	// also append a short trace to a host-local temp file for post-mortem
	if f, err := os.OpenFile("/tmp/git-mediawiki-import.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		fmt.Fprintf(f, "importRevids start: remotename=%s mediaImport=%t revCount=%d tracked=%d fetchFrom=%d\n", remotename, mediaImport, len(revisionIDs), len(tracked), fetchFrom)
		_ = f.Close()
	}
	maxRevs := 50
	mark := 1

	// use writeData helper to emit fast-import data blocks
	// If possible, mirror the fast-import stream to a host-local file for debugging.
	if f, err := os.OpenFile("/tmp/git-mediawiki-fastimport.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		w = io.MultiWriter(w, f)
		// do not close here; let OS close at process exit
	}

	wikiName := rawHostFromURL(apiURL)
	importedAny := false

	for i := 0; i < len(revisionIDs); i += maxRevs {
		end := i + maxRevs
		if end > len(revisionIDs) {
			end = len(revisionIDs)
		}
		emitProgress(w, "Fetching revisions %d-%d of %d", i+1, end, len(revisionIDs))
		batch := revisionIDs[i:end]
		// build revids as pipe-separated
		var idStrs []string
		for _, id := range batch {
			idStrs = append(idStrs, strconv.Itoa(id))
		}
		params := url.Values{}
		params.Set("action", "query")
		params.Set("prop", "revisions")
		params.Set("rvprop", "content|timestamp|comment|user|ids")
		params.Set("revids", strings.Join(idStrs, "|"))
		params.Set("format", "json")
		params.Set("formatversion", "2")
		reqURL := apiURL + "?" + params.Encode()
		resp, err := client.DoRequestWithRetry(httpClient, func() (*http.Request, error) {
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
			return req, nil
		})
		if err != nil {
			return err
		}
		var data map[string]interface{}
		dec := json.NewDecoder(resp.Body)
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		// collect rev records
		type revRec struct {
			Revid     int
			Title     string
			Content   string
			Timestamp string
			User      string
			Comment   string
		}
		var recs []revRec
		seenRevids := make(map[int]bool, len(batch))

		if q, ok := data["query"].(map[string]interface{}); ok {
			if pages, ok := q["pages"].([]interface{}); ok {
				for _, p := range pages {
					if pm, ok := p.(map[string]interface{}); ok {
						title := ""
						if t, ok := pm["title"].(string); ok {
							title = t
						}
						if revs, ok := pm["revisions"].([]interface{}); ok {
							for _, rv := range revs {
								if rm, ok := rv.(map[string]interface{}); ok {
									var rr revRec
									rr.Title = title
									if rn, ok := rm["revid"].(json.Number); ok {
										if n64, err := rn.Int64(); err == nil {
											rr.Revid = int(n64)
										}
									} else if rf, ok := rm["revid"].(float64); ok {
										rr.Revid = int(rf)
									}
									if rr.Revid != 0 {
										seenRevids[rr.Revid] = true
									}
									// try slots->main (new API format uses slots.main.content)
									if slots, ok := rm["slots"].(map[string]interface{}); ok {
										if mainSlot, ok := slots["main"].(map[string]interface{}); ok {
											if c, ok := mainSlot["*"].(string); ok {
												rr.Content = c
											} else if c2, ok := mainSlot["content"].(string); ok {
												rr.Content = c2
											}
										}
									}
									// fallback: legacy keys on the revision object
									if rr.Content == "" {
										if s, ok := rm["*"].(string); ok {
											rr.Content = s
										} else if s2, ok := rm["content"].(string); ok {
											rr.Content = s2
										}
									}
									if ts, ok := rm["timestamp"].(string); ok {
										rr.Timestamp = ts
									}
									if u, ok := rm["user"].(string); ok {
										rr.User = u
									}
									if c, ok := rm["comment"].(string); ok {
										rr.Comment = c
									}
									// only include if tracked; also include File: pages so
									// media can be imported even when tracked map misses them.
									if tracked[rr.Title] || strings.HasPrefix(rr.Title, "File:") {
										recs = append(recs, rr)
									}
								}
							}
						}
					}
				}
			}
		}
		var missingRevids []int
		for _, revid := range batch {
			if !seenRevids[revid] {
				missingRevids = append(missingRevids, revid)
			}
		}
		if len(missingRevids) > 0 {
			sort.Ints(missingRevids)
			msg := fmt.Sprintf("failed to retrieve revision(s): %v", missingRevids)
			if fullImport {
				fmt.Fprintf(ew, "warning: %s\n", msg)
			} else {
				return fmt.Errorf("%s", msg)
			}
		}

		// sort recs by revid ascending
		sort.Slice(recs, func(i, j int) bool { return recs[i].Revid < recs[j].Revid })

		for _, r := range recs {
			importedAny = true
			// commit header
			if fullImport && mark == 1 {
				fmt.Fprintf(w, "reset refs/mediawiki/%s/master\n", remotename)
			}
			fmt.Fprintf(w, "commit refs/mediawiki/%s/master\n", remotename)
			fmt.Fprintf(w, "mark :%d\n", mark)
			epoch := parseMediaWikiTimestamp(r.Timestamp)
			committer := sanitizeIdentName(r.User)
			emailLocal := sanitizeEmailLocal(r.User)
			fmt.Fprintf(w, "committer %s <%s@%s> %d +0000\n", committer, emailLocal, wikiName, epoch)
			// commit message
			if r.Comment == "" {
				r.Comment = fmt.Sprintf("Revision %d of %s", r.Revid, r.Title)
			}
			writeData(w, r.Comment)

			if !fullImport && mark == 1 {
				fmt.Fprintf(w, "from refs/mediawiki/%s/master^0\n", remotename)
			}

			debugf(ew, "importRevids: mark=%d title=%q revid=%d content_len=%d", mark, r.Title, r.Revid, len(r.Content))
			if f, err := os.OpenFile("/tmp/git-mediawiki-import.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
				fmt.Fprintf(f, "revid mark=%d title=%q revid=%d content_len=%d\n", mark, r.Title, r.Revid, len(r.Content))
				_ = f.Close()
			}
			if r.Content != "" {
				filename := client.SmudgeFilename(r.Title) + ".mw"
				path := escapePath(filename)
				fmt.Fprintf(w, "M 644 inline %s\n", path)
				writeData(w, r.Content)
			} else {
				// treat empty content as deletion
				path := escapePath(client.SmudgeFilename(r.Title) + ".mw")
				fmt.Fprintf(w, "D %s\n", path)
			}
			if mediaImport && strings.HasPrefix(r.Title, "File:") {
				if r.Content != "" {
					if err := writeMediaFileInline(w, apiURL, httpClient, r.Title, r.Timestamp); err != nil {
						fmt.Fprintf(ew, "warning: media import for %s failed: %v\n", r.Title, err)
					}
				} else {
					fmt.Fprintf(w, "D %s\n", escapePath(mediaFilenameFromTitle(r.Title)))
				}
			}

			// notes: store mediawiki revision in both the legacy/default
			// refs/notes/commits namespace and our per-remote namespace.
			noteRefs := []string{
				"refs/notes/commits",
				fmt.Sprintf("refs/notes/%s/mediawiki", remotename),
			}
			for _, noteRef := range noteRefs {
				if fullImport && mark == 1 {
					fmt.Fprintf(w, "reset %s\n", noteRef)
				}
				fmt.Fprintf(w, "commit %s\n", noteRef)
				fmt.Fprintf(w, "committer %s <%s@%s> %d +0000\n", committer, emailLocal, wikiName, epoch)
				writeData(w, "Note added by git-mediawiki during import")
				if !fullImport && mark == 1 {
					fmt.Fprintf(w, "from %s^0\n", noteRef)
				}
				// associate note with the commit mark
				fmt.Fprintf(w, "N inline :%d\n", mark)
				writeData(w, fmt.Sprintf("mediawiki_revision: %d", r.Revid))
				fmt.Fprintln(w)
			}

			if mark == 1 || mark%100 == 0 {
				emitProgress(w, "Imported %d revisions", mark)
			}
			mark++
		}
	}
	if fullImport && !importedAny && len(tracked) != 0 {
		var titles []string
		for title := range tracked {
			titles = append(titles, title)
		}
		sort.Strings(titles)
		pages, err := getPagesByTitlesWithClientFunc(httpClient, apiURL, titles)
		if err != nil {
			return err
		}
		emitProgress(w, "No matching revisions found; importing current snapshot of %d tracked pages", len(pages))
		fmt.Fprintf(w, "commit refs/mediawiki/%s/master\n", remotename)
		ts := time.Now().Unix()
		fmt.Fprintf(w, "committer git-mediawiki <noreply@%s> %d +0000\n", wikiName, ts)
		writeData(w, fmt.Sprintf("Import snapshot from %s", apiURL))
		for _, p := range pages {
			filename := client.SmudgeFilename(p.Title) + ".mw"
			path := escapePath(filename)
			if strings.TrimSpace(p.Content) == "" {
				fmt.Fprintf(w, "D %s\n", path)
			} else {
				fmt.Fprintf(w, "M 644 inline %s\n", path)
				writeData(w, p.Content)
			}
			if mediaImport && strings.HasPrefix(p.Title, "File:") {
				if err := writeMediaFileInline(w, apiURL, httpClient, p.Title, ""); err != nil {
					fmt.Fprintf(ew, "warning: media import for %s failed: %v\n", p.Title, err)
				}
			}
		}
		fmt.Fprintln(w)
	}
	emitProgress(w, "Import complete: %d revisions", mark-1)
	return nil
}

// rawHostFromURL returns a sanitized host name for commit email domains
func rawHostFromURL(apiURL string) string {
	// strip scheme
	s := apiURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// remove trailing /api.php
	s = strings.TrimSuffix(s, "/api.php")
	// remove user@ if any
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	// if contains /, cut at first /
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	out := ""
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out += string(r)
		case r == '.' || r == '-':
			out += string(r)
		default:
			out += "-"
		}
	}
	out = strings.Trim(out, ".-")
	if out == "" {
		return "example.invalid"
	}
	return out
}

func appendUniquePages(pages []client.Page, extra []client.Page) []client.Page {
	seen := make(map[string]bool, len(pages))
	for _, p := range pages {
		seen[p.Title] = true
	}
	for _, p := range extra {
		if !seen[p.Title] {
			seen[p.Title] = true
			pages = append(pages, p)
		}
	}
	return pages
}

func mediaFilenameFromTitle(title string) string {
	title = strings.TrimPrefix(title, "File:")
	title = strings.TrimPrefix(title, "Image:")
	return title
}

func augmentPagesWithLinkedMedia(httpClient *http.Client, apiURL string, pages []client.Page) ([]client.Page, error) {
	seen := make(map[string]bool)
	var titles []string
	for _, p := range pages {
		for _, t := range extractFileTitles(p.Content) {
			if !seen[t] {
				seen[t] = true
				titles = append(titles, t)
			}
		}
	}
	if len(titles) == 0 {
		return pages, nil
	}
	mediaPages, err := getPagesByTitlesWithClientFunc(httpClient, apiURL, titles)
	if err != nil {
		return nil, err
	}
	return appendUniquePages(pages, mediaPages), nil
}

func fetchSnapshotPages(httpClient *http.Client, apiURL string, trackedPages, trackedCategories, trackedNamespaces []string) ([]client.Page, error) {
	var (
		pages []client.Page
		err   error
	)
	if len(trackedPages) > 0 {
		pages, err = getPagesByTitlesWithClientFunc(httpClient, apiURL, trackedPages)
		if err != nil {
			return nil, err
		}
	} else if len(trackedCategories) > 0 {
		var titles []string
		for _, cat := range trackedCategories {
			cms, err := client.GetCategoryMembersWithClient(httpClient, apiURL, cat)
			if err != nil {
				return nil, err
			}
			titles = append(titles, cms...)
		}
		if len(titles) > 0 {
			pages, err = getPagesByTitlesWithClientFunc(httpClient, apiURL, titles)
			if err != nil {
				return nil, err
			}
		}
	} else if len(trackedNamespaces) > 0 {
		for _, ns := range trackedNamespaces {
			nsName := strings.ReplaceAll(ns, "_", " ")
			var nsid int
			if n, err := strconv.Atoi(nsName); err == nil {
				nsid = n
			} else if nsName == "(Main)" {
				nsid = 0
			} else {
				nid, err := client.GetNamespaceIDWithClient(httpClient, apiURL, nsName)
				if err != nil {
					return nil, err
				}
				nsid = nid
			}
			pgs, err := getAllPagesContentWithClientFunc(httpClient, apiURL, nsid, 0)
			if err != nil {
				return nil, err
			}
			pages = append(pages, pgs...)
		}
	} else {
		pages, err = getAllPagesContentWithClientFunc(httpClient, apiURL, 0, 0)
		if err != nil {
			return nil, err
		}
	}
	return pages, nil
}

func writeMediaFileInline(w io.Writer, apiURL string, httpClient *http.Client, title, timestamp string) error {
	var (
		data []byte
		err  error
	)
	debugf(nil, "writeMediaFileInline: title=%s timestamp=%s", title, timestamp)
	if timestamp != "" {
		data, err = downloadFileAtTimestampFunc(apiURL, httpClient, title, timestamp)
	} else {
		data, err = downloadFileFunc(apiURL, httpClient, title)
	}
	if err != nil {
		debugf(nil, "writeMediaFileInline: download failed for %s: %v", title, err)
		if f, ferr := os.OpenFile("/tmp/git-mediawiki-writeMedia.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); ferr == nil {
			fmt.Fprintf(f, "download failed %s: %v\n", title, err)
			_ = f.Close()
		}
		return err
	}
	debugf(nil, "writeMediaFileInline: fetched %d bytes for %s", len(data), title)
	if f, ferr := os.OpenFile("/tmp/git-mediawiki-writeMedia.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); ferr == nil {
		fmt.Fprintf(f, "fetched %d bytes for %s\n", len(data), title)
		_ = f.Close()
	}
	fmt.Fprintf(w, "M 644 inline %s\n", escapePath(mediaFilenameFromTitle(title)))
	writeDataBytes(w, data)
	return nil
}

func parseLegacyConfigList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Fields(s)
}

func parseMultiValueConfigList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var values []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		values = append(values, line)
	}
	return values
}

func mergeUniqueStrings(values ...[]string) []string {
	seen := make(map[string]bool)
	var merged []string
	for _, list := range values {
		for _, v := range list {
			if !seen[v] {
				seen[v] = true
				merged = append(merged, v)
			}
		}
	}
	return merged
}

func doImport(w io.Writer, ew io.Writer, remotename, apiURL, rawURL string, refs []string) error {
	// Attempt to read credentials from git config for this remote (so we
	// can import pages from wikis that require login).
	var httpClient *http.Client
	var err error

	// Allow selection of pages via git config similar to the Perl helper.
	// Legacy plural keys keep the historical whitespace-splitting behavior.
	// Singular keys support true multi-value config entries, preserving spaces.
	trackedPagesStr, _, _ := gitExec("config", "--get-all", "remote."+remotename+".pages")
	trackedCategoriesStr, _, _ := gitExec("config", "--get-all", "remote."+remotename+".categories")
	trackedNamespacesStr, _, _ := gitExec("config", "--get-all", "remote."+remotename+".namespaces")
	trackedPageStr, _, _ := gitExec("config", "--get-all", "remote."+remotename+".page")
	trackedCategoryStr, _, _ := gitExec("config", "--get-all", "remote."+remotename+".category")
	trackedNamespaceStr, _, _ := gitExec("config", "--get-all", "remote."+remotename+".namespace")

	trackedPages := mergeUniqueStrings(
		parseLegacyConfigList(trackedPagesStr),
		parseMultiValueConfigList(trackedPageStr),
	)
	trackedCategories := mergeUniqueStrings(
		parseLegacyConfigList(trackedCategoriesStr),
		parseMultiValueConfigList(trackedCategoryStr),
	)
	trackedNamespaces := mergeUniqueStrings(
		parseLegacyConfigList(trackedNamespacesStr),
		parseMultiValueConfigList(trackedNamespaceStr),
	)

	// Read credentials from git config early so we can decide whether
	// to use the test override or perform authenticated fetches.
	username, _, _ := gitExec("config", "--get", "remote."+remotename+".mwLogin")
	password, _, _ := gitExec("config", "--get", "remote."+remotename+".mwPassword")
	domain, _, _ := gitExec("config", "--get", "remote."+remotename+".mwDomain")
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	domain = strings.TrimSpace(domain)

	// Use git credential helper only if one of username/password is missing.
	if username == "" || password == "" {
		gotUser, gotPass, credErr := getCredentialsFromGitCredential(ew, rawURL, username)
		if credErr == nil {
			if username == "" {
				username = gotUser
			}
			if password == "" {
				password = gotPass
			}
		}
	}

	// If we have a username, try to login and create an authenticated http client.
	if username != "" {
		debugf(ew, "attempting login user=%q domain=%q password_len=%d", username, domain, len(password))
		var cClient *http.Client
		var errl error
		if domain != "" {
			cClient, errl = client.Login(apiURL, username, password, domain)
		} else {
			cClient, errl = client.Login(apiURL, username, password)
		}
		if errl != nil {
			// inform credential helper of rejection
			_ = sendCredentialsToGitCredential(ew, "reject", rawURL, username)
			fmt.Fprintf(ew, "login failed: %v\n", errl)
			return ErrAuthFailed
		}
		// login success -> approve credential
		_ = sendCredentialsToGitCredential(ew, "approve", rawURL, username)
		httpClient = cClient
	}

	// Default to revision-based imports for correctness. The legacy single
	// snapshot import is retained only for shallow mode.
	if !getShallow(remotename) {
		// determine last local and last remote revision ids
		lastLocal, _ := getLastLocalRevision(remotename)
		fetchFrom := lastLocal + 1
		lastRemote, err := getLastGlobalRemoteRevFunc(httpClient, apiURL)
		if err != nil {
			return err
		}
		if fetchFrom > lastRemote {
			// nothing to import
			return nil
		}
		// build revision id list
		var revIDs []int
		for r := fetchFrom; r <= lastRemote; r++ {
			revIDs = append(revIDs, r)
		}
		// collect tracked pages map
		trackedMap, err := collectTrackedPagesFunc(httpClient, apiURL, trackedPages, trackedCategories, trackedNamespaces)
		if err != nil {
			return err
		}
		mediaImport := getMediaImport(remotename)
		debugf(ew, "doImport: remotename=%s mediaImport=%t trackedPages=%d trackedCategories=%d trackedNamespaces=%d", remotename, mediaImport, len(trackedPages), len(trackedCategories), len(trackedNamespaces))
		if mediaImport {
			pagesForLinks, err := fetchSnapshotPages(httpClient, apiURL, trackedPages, trackedCategories, trackedNamespaces)
			if err != nil {
				return err
			}
			for _, p := range pagesForLinks {
				for _, title := range extractFileTitles(p.Content) {
					trackedMap[title] = true
				}
			}
		}
		if err := importRevidsFunc(w, ew, remotename, apiURL, httpClient, revIDs, trackedMap, fetchFrom); err != nil {
			return err
		}
		return nil
	}

	// Shallow mode: import a single commit snapshot of tracked pages.
	var pages []client.Page
	if getAllPagesContentFunc != nil && username == "" && password == "" && len(trackedPages) == 0 && len(trackedCategories) == 0 && len(trackedNamespaces) == 0 {
		pages, err = getAllPagesContentFunc(apiURL, 0, 0)
		if err != nil {
			return err
		}
	} else {
		pages, err = fetchSnapshotPages(httpClient, apiURL, trackedPages, trackedCategories, trackedNamespaces)
		if err != nil {
			return err
		}
	}
	if getMediaImport(remotename) {
		pages, err = augmentPagesWithLinkedMedia(httpClient, apiURL, pages)
		if err != nil {
			return err
		}
	}

	// header: commit to private ref (single-commit import)
	emitProgress(w, "Importing snapshot with %d pages", len(pages))
	fmt.Fprintf(w, "commit refs/mediawiki/%s/master\n", remotename)
	if refExists("refs/mediawiki/" + remotename + "/master^0") {
		fmt.Fprintf(w, "from refs/mediawiki/%s/master^0\n", remotename)
	}
	// mark not used here
	ts := time.Now().Unix()
	fmt.Fprintf(w, "committer git-mediawiki <noreply@example.com> %d +0000\n", ts)
	// commit message
	msg := fmt.Sprintf("Import from %s", apiURL)
	writeData(w, msg)
	// write file contents
	for i, p := range pages {
		// debug: report content size for this page to ew
		debugf(ew, "page %q content_size=%d", p.Title, len([]byte(p.Content)))
		filename := client.SmudgeFilename(p.Title) + ".mw"
		path := escapePath(filename)
		content := p.Content
		if strings.TrimSpace(content) == "" {
			// treat empty content as deletion (matches importRevids behavior)
			fmt.Fprintf(w, "D %s\n", path)
		} else {
			fmt.Fprintf(w, "M 644 inline %s\n", path)
			writeData(w, content)
		}
		if getMediaImport(remotename) && strings.HasPrefix(p.Title, "File:") {
			if err := writeMediaFileInline(w, apiURL, httpClient, p.Title, ""); err != nil {
				fmt.Fprintf(ew, "warning: media import for %s failed: %v\n", p.Title, err)
			}
		}
		if i == 0 || (i+1)%100 == 0 {
			emitProgress(w, "Imported %d/%d pages", i+1, len(pages))
		}
	}
	// finish commit body
	fmt.Fprintln(w)
	emitProgress(w, "Import complete: %d pages", len(pages))
	return nil
}

func doPush(w io.Writer, ew io.Writer, remotename, apiURL, rawURL string, refs []string) error {
	pushed := false
	dumbPush := getDumbPush(remotename)
	mediaExport := getMediaExport(remotename)

	// Attempt to read credentials from git config for this remote
	username, _, _ := gitExec("config", "--get", "remote."+remotename+".mwLogin")
	password, _, _ := gitExec("config", "--get", "remote."+remotename+".mwPassword")
	domain, _, _ := gitExec("config", "--get", "remote."+remotename+".mwDomain")
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	domain = strings.TrimSpace(domain)
	var httpClient *http.Client
	if strings.TrimSpace(username) != "" {
		// pass domain if configured
		var c *http.Client
		var errl error
		if strings.TrimSpace(domain) != "" {
			c, errl = client.Login(apiURL, strings.TrimSpace(username), strings.TrimSpace(password), strings.TrimSpace(domain))
		} else {
			c, errl = client.Login(apiURL, strings.TrimSpace(username), strings.TrimSpace(password))
		}
		if errl != nil {
			fmt.Fprintf(ew, "login failed: %v\n", errl)
			return ErrAuthFailed
		}
		httpClient = c
	}

	for _, ref := range refs {
		statusOK := false
		var pushedRevision int64

		// strip optional leading '+' from refspec
		ref = strings.TrimPrefix(ref, "+")
		parts := strings.SplitN(ref, ":", 2)
		if len(parts) != 2 {
			fmt.Fprintf(ew, "invalid refspec: %s\n", ref)
			continue
		}
		local := parts[0]
		remoteRef := parts[1]

		if local == "" {
			fmt.Fprintln(ew, "Cannot delete remote branch on a MediaWiki")
			fmt.Fprintln(w, "error "+remoteRef+" cannot delete")
			continue
		}
		if remoteRef != "refs/heads/master" {
			fmt.Fprintln(ew, "Only push to the branch 'master' is supported on a MediaWiki")
			fmt.Fprintln(w, "error "+remoteRef+" only master allowed")
			continue
		}

		// Get commit for local ref
		commit, _, err := gitExec("rev-parse", local)
		if err != nil {
			fmt.Fprintf(ew, "rev-parse failed for %s: %v\n", local, err)
			continue
		}
		commit = strings.TrimSpace(commit)
		if commit == "" {
			fmt.Fprintf(ew, "empty commit for %s\n", local)
			fmt.Fprintln(w, "error "+remoteRef+" empty commit")
			continue
		}

		baseCommit := ""
		if refExists("refs/mediawiki/" + remotename + "/master^0") {
			baseCommit, _, _ = gitExec("rev-parse", "refs/mediawiki/"+remotename+"/master^0")
			baseCommit = strings.TrimSpace(baseCommit)
		}

		files, err := changedMWFilesFunc(baseCommit, commit)
		if err != nil {
			fmt.Fprintf(ew, "listing changed files failed: %v\n", err)
			continue
		}

		minor := getCommitMinor(commit)

		for _, f := range files {
			if !strings.HasSuffix(f, ".mw") {
				continue
			}
			content, err := showFileFunc(commit, f)
			if err != nil {
				fmt.Fprintf(ew, "reading file %s failed: %v\n", f, err)
				continue
			}
			title := client.FilenameToTitle(filepath.Base(f))
			revid, err := editPage(httpClient, apiURL, title, content, "Pushed from git", minor)
			if err != nil {
				fmt.Fprintf(ew, "edit %s failed: %v\n", title, err)
				continue
			}
			pushedRevision = revid
			fmt.Fprintf(ew, "Pushed page %s\n", title)
			pushed = true
			statusOK = true
		}

		deletedFiles, err := deletedMWFilesFunc(baseCommit, commit)
		if err != nil {
			fmt.Fprintf(ew, "listing deleted files failed: %v\n", err)
		} else {
			for _, f := range deletedFiles {
				title := client.FilenameToTitle(filepath.Base(f))
				revid, err := deletePage(httpClient, apiURL, title, "Deleted from git")
				if err != nil {
					fmt.Fprintf(ew, "delete %s failed: %v\n", title, err)
					continue
				}
				if revid > 0 {
					pushedRevision = revid
				}
				fmt.Fprintf(ew, "Deleted page %s\n", title)
				pushed = true
				statusOK = true
			}
		}

		if mediaExport {
			changedMedia, err := changedMediaFilesFunc(baseCommit, commit)
			if err != nil {
				fmt.Fprintf(ew, "listing changed media files failed: %v\n", err)
			} else {
				for _, f := range changedMedia {
					content, err := showFileBytesFunc(commit, f)
					if err != nil {
						fmt.Fprintf(ew, "reading media file %s failed: %v\n", f, err)
						continue
					}
					revid, err := uploadFile(httpClient, apiURL, filepath.Base(f), content, "Uploaded from git", minor)
					if err != nil {
						fmt.Fprintf(ew, "upload %s failed: %v\n", f, err)
						continue
					}
					if revid > 0 {
						pushedRevision = revid
					}
					fmt.Fprintf(ew, "Uploaded file %s\n", f)
					pushed = true
					statusOK = true
				}
			}

			deletedMedia, err := deletedMediaFilesFunc(baseCommit, commit)
			if err != nil {
				fmt.Fprintf(ew, "listing deleted media files failed: %v\n", err)
			} else {
				for _, f := range deletedMedia {
					title := "File:" + filepath.Base(f)
					revid, err := deletePage(httpClient, apiURL, title, "Deleted from git")
					if err != nil {
						fmt.Fprintf(ew, "delete media %s failed: %v\n", f, err)
						continue
					}
					if revid > 0 {
						pushedRevision = revid
					}
					fmt.Fprintf(ew, "Deleted file %s\n", f)
					pushed = true
					statusOK = true
				}
			}
		}

		if statusOK && !dumbPush {
			if err := updatePushMetadataFunc(remotename, commit, pushedRevision); err != nil {
				fmt.Fprintf(ew, "metadata update failed: %v\n", err)
				fmt.Fprintln(w, "error "+remoteRef+" metadata update failed")
				continue
			}
		}

		if statusOK {
			fmt.Fprintln(w, "ok "+remoteRef)
			if dumbPush {
				fmt.Fprintln(ew, "Metadata not updated because dumbPush is enabled; run git pull --rebase to reimport history.")
			}
		} else {
			fmt.Fprintln(w, "error "+remoteRef+" no pages pushed")
		}
	}

	// Notify Git that push is done
	fmt.Fprintln(w)

	if pushed {
		// nothing more for now; in a real implementation we'd update refs/notes
	}
	return nil
}

func Main() {
	if len(os.Args) != 3 {
		usage()
		os.Exit(1)
	}
	remotename := os.Args[1]
	url := os.Args[2]
	if err := Run(os.Stdin, os.Stdout, os.Stderr, remotename, url); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
