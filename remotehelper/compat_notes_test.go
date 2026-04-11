package remotehelper

import "testing"

func TestGetLastLocalRevisionFallsBackToLegacyNotesRef(t *testing.T) {
	oldGit := gitExecWithStdin
	defer func() { gitExecWithStdin = oldGit }()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 4 && args[0] == "notes" && args[1] == "--ref=origin/mediawiki" {
			return "", "", assertAnError{}
		}
		if len(args) >= 4 && args[0] == "notes" && args[1] == "--ref=commits" {
			return "mediawiki_revision: 42", "", nil
		}
		return "", "", assertAnError{}
	}

	got, err := getLastLocalRevision("origin")
	if err != nil {
		t.Fatalf("getLastLocalRevision failed: %v", err)
	}
	if got != 42 {
		t.Fatalf("unexpected revision: %d", got)
	}
}
