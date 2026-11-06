package remotehelper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClassifyMixedMissingRevisions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.FormValue("list") == "logevents":
			fmt.Fprint(w, `{"query":{"logevents":[]}}`)
		case r.FormValue("revids") == "7":
			fmt.Fprint(w, `{"query":{"pages":[{"title":"Example","deletedrevisions":[{"revid":7}]}]}}`)
		case r.FormValue("revids") == "8":
			fmt.Fprint(w, `{"error":{"code":"drvnosuchrevid"}}`)
		case r.FormValue("revids") == "9":
			fmt.Fprint(w, `{"error":{"code":"drvpermissiondenied"}}`)
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	deleted, unresolved, denied, err := classifyMissingRevisions(server.Client(), server.URL, []int{7, 8, 9}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].Revid != 7 {
		t.Fatalf("deleted revisions: %v", deleted)
	}
	if !reflect.DeepEqual(unresolved, []int{8, 9}) || !denied {
		t.Fatalf("unresolved=%v permissionDenied=%t", unresolved, denied)
	}
}

func TestClassifyMissingRevisionsFailsOnLookupError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if _, _, _, err := classifyMissingRevisions(server.Client(), server.URL, []int{7}, nil); err == nil {
		t.Fatal("expected lookup error")
	}
}
