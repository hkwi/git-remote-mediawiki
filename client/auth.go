package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
)

// Login performs a MediaWiki login flow and returns an http.Client with a cookie jar
// that can be used for authenticated requests. apiURL should be the full path to
// the api.php endpoint (e.g. http://host/api.php).
// Login performs a MediaWiki login flow and returns an http.Client with a cookie jar
// that can be used for authenticated requests. apiURL should be the full path to
// the api.php endpoint (e.g. http://host/api.php).
//
// If provided, an optional domain may be passed as the fourth argument. Example:
// Login(apiURL, username, password, domain)
func Login(apiURL, username, password string, domain ...string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar}

	// Get login token
	tokenVals := url.Values{}
	tokenVals.Set("action", "query")
	tokenVals.Set("meta", "tokens")
	tokenVals.Set("type", "login")
	tokenVals.Set("format", "json")

	tokenURL := apiURL + "?" + tokenVals.Encode()
	resp, err := DoRequestWithRetry(client, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", tokenURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokResp map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&tokResp); err != nil {
		return nil, err
	}

	var token string
	if q, ok := tokResp["query"].(map[string]interface{}); ok {
		if tokens, ok := q["tokens"].(map[string]interface{}); ok {
			if t, ok := tokens["logintoken"].(string); ok {
				token = t
			} else if t, ok := tokens["csrftoken"].(string); ok {
				token = t
			}
		}
	}

	// Post login
	loginVals := url.Values{}
	loginVals.Set("action", "login")
	loginVals.Set("format", "json")
	loginVals.Set("lgname", username)
	loginVals.Set("lgpassword", password)
	if token != "" {
		loginVals.Set("lgtoken", token)
	}
	if len(domain) > 0 && strings.TrimSpace(domain[0]) != "" {
		loginVals.Set("lgdomain", strings.TrimSpace(domain[0]))
	}

	loginBody := loginVals.Encode()
	resp2, err := DoRequestWithRetry(client, func() (*http.Request, error) {
		req2, err := http.NewRequest("POST", apiURL, strings.NewReader(loginBody))
		if err != nil {
			return nil, err
		}
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req2, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		return nil, fmt.Errorf("login request HTTP %d: %s", resp2.StatusCode, string(body))
	}

	var loginResp map[string]interface{}
	dec2 := json.NewDecoder(resp2.Body)
	dec2.UseNumber()
	if err := dec2.Decode(&loginResp); err != nil {
		return nil, err
	}

	// Check common success shapes
	if l, ok := loginResp["login"].(map[string]interface{}); ok {
		if res, ok := l["result"].(string); ok {
			if strings.EqualFold(res, "success") {
				return client, nil
			}
			// Debug: print server response to stderr to aid diagnosis
			_ = json.NewEncoder(os.Stderr).Encode(loginResp)
			return nil, fmt.Errorf("login failed: %s", res)
		}
	}
	if cl, ok := loginResp["clientlogin"].(map[string]interface{}); ok {
		if status, ok := cl["status"].(string); ok {
			if strings.EqualFold(status, "pass") || strings.EqualFold(status, "success") {
				return client, nil
			}
			// Debug: print clientlogin response
			_ = json.NewEncoder(os.Stderr).Encode(loginResp)
			if msg, ok := cl["message"].(string); ok {
				return nil, fmt.Errorf("clientlogin failed: %s", msg)
			}
			return nil, fmt.Errorf("clientlogin failed: %v", cl)
		}
	}

	// As a last resort, consider login successful if the jar has cookies for the host
	if client.Jar != nil {
		if u, err := url.Parse(apiURL); err == nil {
			if cookies := client.Jar.Cookies(&url.URL{Scheme: u.Scheme, Host: u.Host}); len(cookies) > 0 {
				return client, nil
			}
		}
	}

	return nil, fmt.Errorf("unexpected login response: %v", loginResp)
}
