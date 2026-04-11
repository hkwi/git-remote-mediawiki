package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestLoginFlow(t *testing.T) {
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			q := r.URL.Query()
			if q.Get("action") == "query" && q.Get("meta") == "tokens" && q.Get("type") == "login" {
				resp := map[string]interface{}{"query": map[string]interface{}{"tokens": map[string]interface{}{"logintoken": "tok123"}}}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}
		if r.Method == "POST" {
			// login request
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.PostFormValue("lgname") != "user1" {
				t.Fatalf("expected lgname=user1, got %q", r.PostFormValue("lgname"))
			}
			if r.PostFormValue("lgpassword") != "pw1" {
				t.Fatalf("expected lgpassword=pw1, got %q", r.PostFormValue("lgpassword"))
			}
			// set a cookie to simulate session
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "sval", Path: "/"})
			resp := map[string]interface{}{"login": map[string]interface{}{"result": "Success"}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			step++
			return
		}
		t.Fatalf("unexpected request: %s %s %q", r.Method, r.URL.Path, r.URL.RawQuery)
	}))
	defer srv.Close()

	api := srv.URL + "/api.php"
	cl, err := Login(api, "user1", "pw1")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if cl == nil || cl.Jar == nil {
		t.Fatalf("expected client with jar")
	}
	u, _ := url.Parse(api)
	cookies := cl.Jar.Cookies(&url.URL{Scheme: u.Scheme, Host: u.Host})
	if len(cookies) == 0 {
		t.Fatalf("expected cookie after login")
	}
}
