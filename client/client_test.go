package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryAllPagesPagination(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("apcontinue") == "" {
			resp := map[string]interface{}{
				"continue": map[string]interface{}{"apcontinue": "token1"},
				"query": map[string]interface{}{"allpages": []interface{}{
					map[string]interface{}{"pageid": 1, "title": "P1"},
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		resp := map[string]interface{}{
			"query": map[string]interface{}{"allpages": []interface{}{
				map[string]interface{}{"pageid": 2, "title": "P2"},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}

	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	pages, err := QueryAllPages(srv.URL+"/api.php", 0, 10)
	if err != nil {
		t.Fatalf("QueryAllPages error: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	if pages[0]["title"] != "P1" || pages[1]["title"] != "P2" {
		t.Fatalf("unexpected pages: %#v", pages)
	}
}

func TestGetAllPagesContentGenerator(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// first page
		if q.Get("gapcontinue") == "" {
			resp := map[string]interface{}{
				"continue": map[string]interface{}{"gapcontinue": "token1"},
				"query": map[string]interface{}{"pages": []interface{}{
					map[string]interface{}{
						"pageid": 1,
						"title":  "Page1",
						"revisions": []interface{}{
							map[string]interface{}{"slots": map[string]interface{}{"main": map[string]interface{}{"*": "content1"}}},
						},
					},
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// second (final) page
		resp := map[string]interface{}{
			"query": map[string]interface{}{"pages": []interface{}{
				map[string]interface{}{
					"pageid": 2,
					"title":  "Page2",
					"revisions": []interface{}{
						map[string]interface{}{"slots": map[string]interface{}{"main": map[string]interface{}{"*": "content2"}}},
					},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}

	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	pages, err := GetAllPagesContent(srv.URL+"/api.php", 0, 1)
	if err != nil {
		t.Fatalf("GetAllPagesContent error: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	if pages[0].Title != "Page1" || pages[0].Content != "content1" {
		t.Fatalf("unexpected first page: %#v", pages[0])
	}
	if pages[1].Title != "Page2" || pages[1].Content != "content2" {
		t.Fatalf("unexpected second page: %#v", pages[1])
	}
}
