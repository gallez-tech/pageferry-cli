package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Origin     string
	APIKey     string
	Version    string
	HTTPClient *http.Client
}

type Identity struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	APIKeyID    string `json:"apiKeyId"`
	APIKeyName  string `json:"apiKeyName"`
}

type UploadRequest struct {
	HTML             string         `json:"html"`
	Filename         string         `json:"filename"`
	DraftID          string         `json:"draftId,omitempty"`
	Description      *string        `json:"description,omitempty"`
	HostingMode      string         `json:"hostingMode,omitempty"`
	ExpiresInSeconds *int64         `json:"expiresInSeconds,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

type UploadResponse struct {
	DraftID       string   `json:"draftId"`
	VersionID     string   `json:"versionId"`
	VersionNumber int      `json:"versionNumber"`
	Title         string   `json:"title"`
	PublicURL     string   `json:"publicUrl"`
	RawURL        string   `json:"rawUrl"`
	VersionURL    string   `json:"versionUrl"`
	HostingMode   string   `json:"hostingMode"`
	WorkersDevURL *string  `json:"workersDevUrl"`
	ExpiresAt     *string  `json:"expiresAt"`
	Warnings      []string `json:"warnings"`
}

type Draft struct {
	DraftID             string  `json:"draftId"`
	Title               string  `json:"title"`
	Description         *string `json:"description"`
	RepoOrg             *string `json:"repoOrg"`
	RepoName            *string `json:"repoName"`
	RepoHost            *string `json:"repoHost"`
	LatestVersionNumber *int    `json:"latestVersionNumber"`
	VersionCount        int     `json:"versionCount"`
	CreatedAt           string  `json:"createdAt"`
	UpdatedAt           string  `json:"updatedAt"`
	LatestVersionAt     *string `json:"latestVersionAt"`
	Disabled            bool    `json:"disabled"`
	HostingMode         string  `json:"hostingMode"`
	ExpiresAt           *string `json:"expiresAt"`
	Expired             bool    `json:"expired"`
	PublicURL           string  `json:"publicUrl"`
	RawURL              string  `json:"rawUrl"`
}

type APIError struct {
	Status    int
	ErrorText string   `json:"error"`
	Errors    []string `json:"errors"`
}

func (e *APIError) Error() string {
	parts := make([]string, 0, 1+len(e.Errors))
	if e.ErrorText != "" {
		parts = append(parts, e.ErrorText)
	}
	parts = append(parts, e.Errors...)
	if len(parts) == 0 {
		return fmt.Sprintf("server returned HTTP %d", e.Status)
	}
	return strings.Join(parts, "\n  - ")
}

func ValidateOrigin(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid API URL %q (expected an http or https origin)", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("invalid API URL %q (paths, queries, and fragments are not allowed)", raw)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func (c *Client) Me(ctx context.Context) (Identity, error) {
	var response Identity
	err := c.do(ctx, http.MethodGet, "/api/me", nil, &response)
	return response, err
}

func (c *Client) Upload(ctx context.Context, request UploadRequest) (UploadResponse, error) {
	var response UploadResponse
	err := c.do(ctx, http.MethodPost, "/api/uploads", request, &response)
	return response, err
}

func (c *Client) Drafts(ctx context.Context) ([]Draft, error) {
	var response struct {
		Drafts []Draft `json:"drafts"`
	}
	err := c.do(ctx, http.MethodGet, "/api/drafts", nil, &response)
	return response.Drafts, err
}

func (c *Client) do(ctx context.Context, method, path string, body, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Origin+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("User-Agent", "pageferry/"+c.Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 3<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiErr := &APIError{Status: response.StatusCode}
		_ = json.Unmarshal(data, apiErr)
		if response.StatusCode == http.StatusNotImplemented && strings.Contains(strings.ToLower(apiErr.ErrorText), "workers.dev") {
			return errors.New("dedicated workers.dev hosting is unavailable; the account may have exhausted its Worker quota or the server may not have enabled this feature: " + apiErr.ErrorText)
		}
		return apiErr
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode server response: %w", err)
	}
	return nil
}
