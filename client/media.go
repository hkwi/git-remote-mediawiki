package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// GetFileURL resolves the current download URL for a MediaWiki File: page.
func GetFileURL(apiURL string, httpClient *http.Client, title string) (string, error) {
	return getFileURL(apiURL, httpClient, title, "", "")
}

// GetFileURLAtTimestamp resolves the download URL for the file revision at the
// given MediaWiki timestamp.
func GetFileURLAtTimestamp(apiURL string, httpClient *http.Client, title, timestamp string) (string, error) {
	return getFileURL(apiURL, httpClient, title, timestamp, timestamp)
}

func getFileURL(apiURL string, httpClient *http.Client, title, start, end string) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("format", "json")
	params.Set("formatversion", "2")
	params.Set("prop", "imageinfo")
	params.Set("iiprop", "url")
	params.Set("iilimit", "1")
	params.Set("titles", title)
	if start != "" {
		params.Set("iistart", start)
	}
	if end != "" {
		params.Set("iiend", end)
	}

	reqURL := apiURL + "?" + params.Encode()
	resp, err := DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req, nil
	})
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return "", fmt.Errorf("file info request HTTP %d: %s", resp.StatusCode, string(body))
	}
	var data map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		resp.Body.Close()
		return "", err
	}
	resp.Body.Close()

	if q, ok := data["query"].(map[string]interface{}); ok {
		if pages, ok := q["pages"].([]interface{}); ok {
			for _, p := range pages {
				pm, ok := p.(map[string]interface{})
				if !ok {
					continue
				}
				if infos, ok := pm["imageinfo"].([]interface{}); ok && len(infos) > 0 {
					if info0, ok := infos[0].(map[string]interface{}); ok {
						if fileURL, ok := info0["url"].(string); ok {
							return fileURL, nil
						}
					}
				}
			}
		}
	}
	return "", fmt.Errorf("file URL not found for %s", title)
}

// DownloadFile fetches the current file content for a MediaWiki File: page.
func DownloadFile(apiURL string, httpClient *http.Client, title string) ([]byte, error) {
	return downloadFileURL(apiURL, httpClient, func() (string, error) {
		return GetFileURL(apiURL, httpClient, title)
	})
}

// DownloadFileAtTimestamp fetches the file content for the file revision at
// the given MediaWiki timestamp.
func DownloadFileAtTimestamp(apiURL string, httpClient *http.Client, title, timestamp string) ([]byte, error) {
	return downloadFileURL(apiURL, httpClient, func() (string, error) {
		return GetFileURLAtTimestamp(apiURL, httpClient, title, timestamp)
	})
}

func downloadFileURL(apiURL string, httpClient *http.Client, resolve func() (string, error)) ([]byte, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	fileURL, err := resolve()
	if err != nil {
		return nil, err
	}
	resp, err := DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", fileURL, nil)
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
		return nil, fmt.Errorf("file download HTTP %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

// UploadFile uploads or replaces a media file on the wiki.
func UploadFile(apiURL string, httpClient *http.Client, filename string, content []byte, comment string) (int64, error) {
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

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("action", "upload")
	_ = writer.WriteField("format", "json")
	_ = writer.WriteField("filename", filepath.Base(filename))
	_ = writer.WriteField("ignorewarnings", "1")
	if comment != "" {
		_ = writer.WriteField("comment", comment)
	}
	if token != "" {
		_ = writer.WriteField("token", token)
	}
	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(content); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}

	resp2, err := DoRequestWithRetry(httpClient, func() (*http.Request, error) {
		req2, err := http.NewRequest("POST", apiURL, bytes.NewReader(body.Bytes()))
		if err != nil {
			return nil, err
		}
		req2.Header.Set("Content-Type", writer.FormDataContentType())
		req2.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req2, nil
	})
	if err != nil {
		return 0, err
	}
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		return 0, fmt.Errorf("upload request HTTP %d: %s", resp2.StatusCode, string(body))
	}
	var uploadResp map[string]interface{}
	dec2 := json.NewDecoder(resp2.Body)
	dec2.UseNumber()
	if err := dec2.Decode(&uploadResp); err != nil {
		resp2.Body.Close()
		return 0, err
	}
	resp2.Body.Close()

	if up, ok := uploadResp["upload"].(map[string]interface{}); ok {
		if result, ok := up["result"].(string); ok && !strings.EqualFold(result, "success") && !strings.EqualFold(result, "warning") {
			return 0, fmt.Errorf("upload failed: %s", result)
		}
		if imginfo, ok := up["imageinfo"].(map[string]interface{}); ok {
			if nr, ok := imginfo["timestamp"].(string); ok && nr != "" {
				return 1, nil
			}
		}
	}
	return 0, nil
}
