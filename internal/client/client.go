// Package client is the HTTP client used by the issues CLI subcommands. It
// targets $ISSUES_URL (default http://127.0.0.1:8787) with an optional
// $ISSUES_TOKEN bearer.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Socrates10101/issues-manager/internal/store"
)

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New() *Client {
	base := os.Getenv("ISSUES_URL")
	if base == "" {
		base = "http://127.0.0.1:8787"
	}
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: os.Getenv("ISSUES_TOKEN"),
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed (is the server running? ISSUES_URL=%s): %w", c.base, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("server error %d: %s", resp.StatusCode, e.Error)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) List(project, status string, archived *bool) ([]store.Issue, error) {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	if status != "" {
		q.Set("status", status)
	}
	if archived != nil {
		q.Set("archived", fmt.Sprintf("%t", *archived))
	}
	var out []store.Issue
	path := "/issues"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return out, c.do(http.MethodGet, path, nil, &out)
}

func (c *Client) Get(project string, number int) (store.Issue, error) {
	var out store.Issue
	return out, c.do(http.MethodGet, fmt.Sprintf("/issues/%s/%d", project, number), nil, &out)
}

func (c *Client) Create(project, title, body string) (store.Issue, error) {
	var out store.Issue
	in := map[string]string{"project": project, "title": title, "body": body}
	return out, c.do(http.MethodPost, "/issues", in, &out)
}

func (c *Client) SetStatus(project string, number int, status string) (store.Issue, error) {
	var out store.Issue
	in := map[string]string{"status": status}
	return out, c.do(http.MethodPatch, fmt.Sprintf("/issues/%s/%d", project, number), in, &out)
}

func (c *Client) Archive(project string, number int) (store.Issue, error) {
	var out store.Issue
	return out, c.do(http.MethodPost, fmt.Sprintf("/issues/%s/%d/archive", project, number), nil, &out)
}
