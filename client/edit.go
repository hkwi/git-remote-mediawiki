package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func mediaWikiAPIError(resp map[string]interface{}) error {
	errVal, ok := resp["error"].(map[string]interface{})
	if !ok {
		return nil
	}
	code, _ := errVal["code"].(string)
	info, _ := errVal["info"].(string)
	switch {
	case code != "" && info != "":
		return fmt.Errorf("mediawiki API error %s: %s", code, info)
	case info != "":
		return fmt.Errorf("mediawiki API error: %s", info)
	case code != "":
		return fmt.Errorf("mediawiki API error: %s", code)
	default:
		return fmt.Errorf("mediawiki API error")
	}
}

// EditPage performs a simple edit (create/update) of a page. If httpClient is
// nil, http.DefaultClient is used. Returns the new revision id when available.
func EditPage(apiURL string, httpClient *http.Client, title, content, summary string, minor bool) (int64, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// Get CSRF token
	tokenVals := url.Values{}
	tokenVals.Set("action", "query")
	tokenVals.Set("meta", "tokens")
	tokenVals.Set("type", "csrf")
	tokenVals.Set("format", "json")

	tokenURL := apiURL + "?" + tokenVals.Encode()
	resp, err := DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", tokenURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req, nil
	})
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return 0, fmt.Errorf("token request HTTP %d: %s", resp.StatusCode, string(body))
	}
	var tokResp map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&tokResp); err != nil {
		resp.Body.Close()
		return 0, err
	}
	resp.Body.Close()

	var token string
	if q, ok := tokResp["query"].(map[string]interface{}); ok {
		if tokens, ok := q["tokens"].(map[string]interface{}); ok {
			if t, ok := tokens["csrftoken"].(string); ok {
				token = t
			}
		}
	}

	// Build edit POST
	vals := url.Values{}
	vals.Set("action", "edit")
	vals.Set("format", "json")
	vals.Set("title", title)
	vals.Set("text", content)
	if summary != "" {
		vals.Set("summary", summary)
	}
	if minor {
		vals.Set("minor", "1")
	}
	if token != "" {
		vals.Set("token", token)
	}

	editBody := vals.Encode()
	resp2, err := DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req2, err := http.NewRequest("POST", apiURL, strings.NewReader(editBody))
		if err != nil {
			return nil, err
		}
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req2, nil
	})
	if err != nil {
		return 0, err
	}
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		return 0, fmt.Errorf("edit request HTTP %d: %s", resp2.StatusCode, string(body))
	}
	var editResp map[string]interface{}
	dec2 := json.NewDecoder(resp2.Body)
	dec2.UseNumber()
	if err := dec2.Decode(&editResp); err != nil {
		resp2.Body.Close()
		return 0, err
	}
	resp2.Body.Close()

	if err := mediaWikiAPIError(editResp); err != nil {
		return 0, err
	}
	if e, ok := editResp["edit"].(map[string]interface{}); ok {
		if result, ok := e["result"].(string); ok && !strings.EqualFold(result, "success") {
			return 0, fmt.Errorf("edit failed: %s", result)
		}
		if nr, ok := e["newrevid"].(json.Number); ok {
			if id, err := nr.Int64(); err == nil {
				return id, nil
			}
		} else if nrf, ok := e["newrevid"].(float64); ok {
			return int64(nrf), nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("edit failed: missing edit response")
}

// DeletePage deletes a page from MediaWiki. If httpClient is nil,
// http.DefaultClient is used. Returns the deleted revision id when available.
func DeletePage(apiURL string, httpClient *http.Client, title, reason string) (int64, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	tokenVals := url.Values{}
	tokenVals.Set("action", "query")
	tokenVals.Set("meta", "tokens")
	tokenVals.Set("type", "csrf")
	tokenVals.Set("format", "json")

	tokenURL := apiURL + "?" + tokenVals.Encode()
	resp, err := DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", tokenURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req, nil
	})
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return 0, fmt.Errorf("token request HTTP %d: %s", resp.StatusCode, string(body))
	}
	var tokResp map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&tokResp); err != nil {
		resp.Body.Close()
		return 0, err
	}
	resp.Body.Close()

	var token string
	if q, ok := tokResp["query"].(map[string]interface{}); ok {
		if tokens, ok := q["tokens"].(map[string]interface{}); ok {
			if t, ok := tokens["csrftoken"].(string); ok {
				token = t
			}
		}
	}

	vals := url.Values{}
	vals.Set("action", "delete")
	vals.Set("format", "json")
	vals.Set("title", title)
	if reason != "" {
		vals.Set("reason", reason)
	}
	if token != "" {
		vals.Set("token", token)
	}

	deleteBody := vals.Encode()
	resp2, err := DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req2, err := http.NewRequest("POST", apiURL, strings.NewReader(deleteBody))
		if err != nil {
			return nil, err
		}
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req2, nil
	})
	if err != nil {
		return 0, err
	}
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		return 0, fmt.Errorf("delete request HTTP %d: %s", resp2.StatusCode, string(body))
	}
	var delResp map[string]interface{}
	dec2 := json.NewDecoder(resp2.Body)
	dec2.UseNumber()
	if err := dec2.Decode(&delResp); err != nil {
		resp2.Body.Close()
		return 0, err
	}
	resp2.Body.Close()

	if err := mediaWikiAPIError(delResp); err != nil {
		return 0, err
	}
	if d, ok := delResp["delete"].(map[string]interface{}); ok {
		if logid, ok := d["logid"].(json.Number); ok {
			if id, err := logid.Int64(); err == nil {
				return id, nil
			}
		} else if logidf, ok := d["logid"].(float64); ok {
			return int64(logidf), nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("delete failed: missing delete response")
}
