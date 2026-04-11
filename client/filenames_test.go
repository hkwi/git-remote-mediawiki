package client

import "testing"

func TestCleanFilename(t *testing.T) {
	in := "%2Ffoo[bar"
	want := "/foo_%_5bbar"
	if got := CleanFilename(in); got != want {
		t.Fatalf("CleanFilename(%q) = %q; want %q", in, got, want)
	}
}

func TestSmudgeFilename(t *testing.T) {
	// space -> underscore
	if got := SmudgeFilename("a b"); got != "a_b" {
		t.Fatalf("SmudgeFilename space -> underscore: got %q", got)
	}
	// decode encoded forbidden char
	if got := SmudgeFilename("_%_5b"); got != "[" {
		t.Fatalf("SmudgeFilename decode: got %q", got)
	}
	// slash replacement
	if got := SmudgeFilename("a/b"); got != "a%2Fb" {
		t.Fatalf("SmudgeFilename slash: got %q", got)
	}
}
