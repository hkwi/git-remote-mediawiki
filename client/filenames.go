package client

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const SlashReplacement = "%2F"

// CleanFilename implements the same variant of cleaning used by the reference
// implementation: it converts the slash-replacement back to '/', and encodes
// forbidden characters ([ ] { } |) as "_%_<hex>" (lowercase hex).
func CleanFilename(filename string) string {
	s := strings.ReplaceAll(filename, SlashReplacement, "/")
	out := ""
	for _, r := range s {
		switch r {
		case '[', ']', '{', '}', '|':
			out += fmt.Sprintf("_%%_%x", r)
		default:
			out += string(r)
		}
	}
	return out
}

// SmudgeFilename implements the reverse mapping used in the reference: it
// replaces '/' with the slash replacement, spaces with underscores, and
// decodes sequences like "_%_5b" back to the original character.
func SmudgeFilename(filename string) string {
	s := strings.ReplaceAll(filename, "/", SlashReplacement)
	s = strings.ReplaceAll(s, " ", "_")
	re := regexp.MustCompile(`_%_([0-9a-fA-F]{2})`)
	s = re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		v, err := strconv.ParseInt(sub[1], 16, 0)
		if err != nil {
			return m
		}
		return string(rune(v))
	})
	return s
}

// FilenameToTitle converts a repository filename (the inverse of SmudgeFilename
// plus an optional ".mw" suffix) back to a MediaWiki page title.
func FilenameToTitle(filename string) string {
	s := filename
	s = strings.TrimSuffix(s, ".mw")
	// Reverse the slash replacement first
	s = strings.ReplaceAll(s, SlashReplacement, "/")
	// Convert underscores back to spaces
	s = strings.ReplaceAll(s, "_", " ")
	// Decode any _%_XX sequences created by CleanFilename
	re := regexp.MustCompile(`_%_([0-9a-fA-F]{2})`)
	s = re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		v, err := strconv.ParseInt(sub[1], 16, 0)
		if err != nil {
			return m
		}
		return string(rune(v))
	})
	return s
}
