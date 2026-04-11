package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Page represents a MediaWiki page with its latest content.
type Page struct {
	PageID  int64
	Title   string
	Content string
}

// QueryAllPages queries MediaWiki's 'allpages' list and follows pagination.
func QueryAllPages(apiURL string, namespace int, limit int) ([]map[string]interface{}, error) {
	params := url.Values{}
	params.Set("action", "query")
	params.Set("format", "json")
	params.Set("list", "allpages")
	if limit <= 0 {
		params.Set("aplimit", "max")
	} else {
		params.Set("aplimit", fmt.Sprintf("%d", limit))
	}
	params.Set("apnamespace", fmt.Sprintf("%d", namespace))

	var all []map[string]interface{}
	client := http.DefaultClient

	for {
		reqURL := apiURL + "?" + params.Encode()
		resp, err := DoRequestWithRetry(client, func() (*http.Request, error) {
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
			return req, nil
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		var data map[string]interface{}
		dec := json.NewDecoder(resp.Body)
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		if q, ok := data["query"].(map[string]interface{}); ok {
			if ap, ok := q["allpages"].([]interface{}); ok {
				for _, v := range ap {
					if m, ok := v.(map[string]interface{}); ok {
						all = append(all, m)
					}
				}
			}
		}

		// Handle continuation keys: copy them into params for the next request.
		if cont, ok := data["continue"].(map[string]interface{}); ok {
			for k, v := range cont {
				params.Set(k, fmt.Sprint(v))
			}
			continue
		}
		break
	}
	return all, nil
}

// GetAllPagesContent uses generator=allpages + prop=revisions to fetch pages with their content.
func GetAllPagesContent(apiURL string, namespace int, limit int) ([]Page, error) {
	params := url.Values{}
	params.Set("action", "query")
	params.Set("format", "json")
	params.Set("formatversion", "2")
	params.Set("generator", "allpages")
	params.Set("gapnamespace", fmt.Sprintf("%d", namespace))
	if limit <= 0 {
		params.Set("gaplimit", "max")
	} else {
		params.Set("gaplimit", fmt.Sprintf("%d", limit))
	}
	params.Set("prop", "revisions")
	params.Set("rvprop", "content")
	params.Set("rvslots", "main")

	var result []Page
	client := http.DefaultClient

	for {
		reqURL := apiURL + "?" + params.Encode()
		resp, err := DoRequestWithRetry(client, func() (*http.Request, error) {
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
			return req, nil
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		var data map[string]interface{}
		dec := json.NewDecoder(resp.Body)
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		if q, ok := data["query"].(map[string]interface{}); ok {
			if pages, ok := q["pages"].([]interface{}); ok {
				for _, p := range pages {
					if pm, ok := p.(map[string]interface{}); ok {
						var pg Page
						if idNum, ok := pm["pageid"].(json.Number); ok {
							if id64, err := idNum.Int64(); err == nil {
								pg.PageID = id64
							}
						} else if idf, ok := pm["pageid"].(float64); ok {
							pg.PageID = int64(idf)
						}
						if t, ok := pm["title"].(string); ok {
							pg.Title = t
						}
						// revisions -> first -> slots -> main -> *
						if revs, ok := pm["revisions"].([]interface{}); ok && len(revs) > 0 {
							if rev0, ok := revs[0].(map[string]interface{}); ok {
								// try slots->main (new format uses "content", legacy used "*")
								if slots, ok := rev0["slots"].(map[string]interface{}); ok {
									if mainSlot, ok := slots["main"].(map[string]interface{}); ok {
										if c, ok := mainSlot["*"].(string); ok {
											pg.Content = c
										} else if c2, ok := mainSlot["content"].(string); ok {
											pg.Content = c2
										}
									}
								}
								// fallback: direct 'content' or '*' on rev (legacy responses)
								if pg.Content == "" {
									if c, ok := rev0["*"].(string); ok {
										pg.Content = c
									} else if c2, ok := rev0["content"].(string); ok {
										pg.Content = c2
									}
								}
							}
						}
						result = append(result, pg)
					}
				}
			}
		}

		if cont, ok := data["continue"].(map[string]interface{}); ok {
			for k, v := range cont {
				params.Set(k, fmt.Sprint(v))
			}
			continue
		}
		break
	}
	return result, nil
}

// GetAllPagesContentWithClient is like GetAllPagesContent but allows an
// explicit HTTP client (for authenticated requests). If httpClient is nil
// http.DefaultClient is used.
func GetAllPagesContentWithClient(httpClient *http.Client, apiURL string, namespace int, limit int) ([]Page, error) {
	params := url.Values{}
	params.Set("action", "query")
	params.Set("format", "json")
	params.Set("formatversion", "2")
	params.Set("generator", "allpages")
	params.Set("gapnamespace", fmt.Sprintf("%d", namespace))
	if limit <= 0 {
		params.Set("gaplimit", "max")
	} else {
		params.Set("gaplimit", fmt.Sprintf("%d", limit))
	}
	params.Set("prop", "revisions")
	params.Set("rvprop", "content")
	params.Set("rvslots", "main")

	var result []Page
	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}

	for {
		reqURL := apiURL + "?" + params.Encode()
		resp, err := DoRequestWithRetry(client, func() (*http.Request, error) {
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
			return req, nil
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		var data map[string]interface{}
		dec := json.NewDecoder(resp.Body)
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		if q, ok := data["query"].(map[string]interface{}); ok {
			if pages, ok := q["pages"].([]interface{}); ok {
				for _, p := range pages {
					if pm, ok := p.(map[string]interface{}); ok {
						var pg Page
						if idNum, ok := pm["pageid"].(json.Number); ok {
							if id64, err := idNum.Int64(); err == nil {
								pg.PageID = id64
							}
						} else if idf, ok := pm["pageid"].(float64); ok {
							pg.PageID = int64(idf)
						}
						if t, ok := pm["title"].(string); ok {
							pg.Title = t
						}
						// revisions -> first -> slots -> main -> *
						if revs, ok := pm["revisions"].([]interface{}); ok && len(revs) > 0 {
							if rev0, ok := revs[0].(map[string]interface{}); ok {
								// try slots->main (new format uses "content", legacy used "*")
								if slots, ok := rev0["slots"].(map[string]interface{}); ok {
									if mainSlot, ok := slots["main"].(map[string]interface{}); ok {
										if c, ok := mainSlot["*"].(string); ok {
											pg.Content = c
										} else if c2, ok := mainSlot["content"].(string); ok {
											pg.Content = c2
										}
									}
								}
								// fallback: direct 'content' or '*' on rev (legacy responses)
								if pg.Content == "" {
									if c, ok := rev0["*"].(string); ok {
										pg.Content = c
									} else if c2, ok := rev0["content"].(string); ok {
										pg.Content = c2
									}
								}
							}
						}
						result = append(result, pg)
					}
				}
			}
		}

		if cont, ok := data["continue"].(map[string]interface{}); ok {
			for k, v := range cont {
				params.Set(k, fmt.Sprint(v))
			}
			continue
		}
		break
	}
	return result, nil
}

// GetPagesByTitlesWithClient fetches the full content for the given page titles.
// Titles will be chunked to avoid overly-long requests.
func GetPagesByTitlesWithClient(httpClient *http.Client, apiURL string, titles []string) ([]Page, error) {
	const chunkSize = 50
	var result []Page
	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}

	for i := 0; i < len(titles); i += chunkSize {
		end := i + chunkSize
		if end > len(titles) {
			end = len(titles)
		}
		chunk := titles[i:end]

		params := url.Values{}
		params.Set("action", "query")
		params.Set("format", "json")
		params.Set("formatversion", "2")
		params.Set("prop", "revisions")
		params.Set("rvprop", "content")
		params.Set("rvslots", "main")
		params.Set("titles", strings.Join(chunk, "|"))

		for {
			reqURL := apiURL + "?" + params.Encode()
			resp, err := DoRequestWithRetry(client, func() (*http.Request, error) {
				req, err := http.NewRequest("GET", reqURL, nil)
				if err != nil {
					return nil, err
				}
				req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
				return req, nil
			})
			if err != nil {
				return nil, err
			}
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			}
			var data map[string]interface{}
			dec := json.NewDecoder(resp.Body)
			dec.UseNumber()
			if err := dec.Decode(&data); err != nil {
				resp.Body.Close()
				return nil, err
			}
			resp.Body.Close()

			if q, ok := data["query"].(map[string]interface{}); ok {
				if pages, ok := q["pages"].([]interface{}); ok {
					for _, p := range pages {
						if pm, ok := p.(map[string]interface{}); ok {
							var pg Page
							if idNum, ok := pm["pageid"].(json.Number); ok {
								if id64, err := idNum.Int64(); err == nil {
									pg.PageID = id64
								}
							} else if idf, ok := pm["pageid"].(float64); ok {
								pg.PageID = int64(idf)
							}
							if t, ok := pm["title"].(string); ok {
								pg.Title = t
							}
							if revs, ok := pm["revisions"].([]interface{}); ok && len(revs) > 0 {
								if rev0, ok := revs[0].(map[string]interface{}); ok {
									if slots, ok := rev0["slots"].(map[string]interface{}); ok {
										if mainSlot, ok := slots["main"].(map[string]interface{}); ok {
											if c, ok := mainSlot["*"].(string); ok {
												pg.Content = c
											} else if c2, ok := mainSlot["content"].(string); ok {
												pg.Content = c2
											}
										}
									}
									if pg.Content == "" {
										if c, ok := rev0["*"].(string); ok {
											pg.Content = c
										} else if c2, ok := rev0["content"].(string); ok {
											pg.Content = c2
										}
									}
								}
							}
							result = append(result, pg)
						}
					}
				}
			}

			if cont, ok := data["continue"].(map[string]interface{}); ok {
				for k, v := range cont {
					params.Set(k, fmt.Sprint(v))
				}
				continue
			}
			break
		}
	}
	return result, nil
}

// GetCategoryMembersWithClient returns page titles for a given category.
func GetCategoryMembersWithClient(httpClient *http.Client, apiURL string, category string) ([]string, error) {
	var result []string
	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("format", "json")
	params.Set("list", "categorymembers")
	// ensure category title has prefix
	title := category
	if !strings.Contains(title, ":") {
		title = "Category:" + title
	}
	params.Set("cmtitle", title)
	params.Set("cmlimit", "max")

	for {
		reqURL := apiURL + "?" + params.Encode()
		resp, err := DoRequestWithRetry(client, func() (*http.Request, error) {
			req, err := http.NewRequest("GET", reqURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
			return req, nil
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		var data map[string]interface{}
		dec := json.NewDecoder(resp.Body)
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		if q, ok := data["query"].(map[string]interface{}); ok {
			if cms, ok := q["categorymembers"].([]interface{}); ok {
				for _, it := range cms {
					if im, ok := it.(map[string]interface{}); ok {
						if t, ok := im["title"].(string); ok {
							result = append(result, t)
						}
					}
				}
			}
		}

		if cont, ok := data["continue"].(map[string]interface{}); ok {
			for k, v := range cont {
				params.Set(k, fmt.Sprint(v))
			}
			continue
		}
		break
	}
	return result, nil
}

// GetNamespaceIDWithClient looks up namespace name and returns its numeric id.
// Returns -1 if not found.
func GetNamespaceIDWithClient(httpClient *http.Client, apiURL string, name string) (int, error) {
	if name == "(Main)" || name == "Main" || name == "" {
		return 0, nil
	}
	// try parse number first
	if n, err := strconv.Atoi(name); err == nil {
		return n, nil
	}

	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("format", "json")
	params.Set("meta", "siteinfo")
	params.Set("siprop", "namespaces")

	reqURL := apiURL + "?" + params.Encode()
	resp, err := DoRequestWithRetry(client, func() (*http.Request, error) {
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "git-mediawiki-go/0.1")
		return req, nil
	})
	if err != nil {
		return -1, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return -1, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var data map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		resp.Body.Close()
		return -1, err
	}
	resp.Body.Close()

	if q, ok := data["query"].(map[string]interface{}); ok {
		if nsmap, ok := q["namespaces"].(map[string]interface{}); ok {
			for k, v := range nsmap {
				if m, ok := v.(map[string]interface{}); ok {
					if nm, ok := m["*"].(string); ok {
						if nm == name {
							if id, err := strconv.Atoi(k); err == nil {
								return id, nil
							}
						}
					}
					if cnm, ok := m["canonical"].(string); ok {
						if cnm == name {
							if id, err := strconv.Atoi(k); err == nil {
								return id, nil
							}
						}
					}
				}
			}
		}
	}
	return -1, fmt.Errorf("namespace not found: %s", name)
}
