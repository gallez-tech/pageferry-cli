package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
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
	HTML             string            `json:"html,omitempty"`
	Filename         string            `json:"filename,omitempty"`
	Files            []string          `json:"files,omitempty"`
	DraftID          string            `json:"draftId,omitempty"`
	Description      *string           `json:"description,omitempty"`
	HostingMode      string            `json:"hostingMode,omitempty"`
	ExpiresInSeconds *int64            `json:"expiresInSeconds,omitempty"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
	Access           *UploadAccess     `json:"access,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	Secrets          map[string]string `json:"secrets,omitempty"`
}

type UploadAccess struct {
	Mode     string   `json:"mode,omitempty"`
	Password string   `json:"password,omitempty"`
	Emails   []string `json:"emails,omitempty"`
}

// DraftAccessRequest changes a draft's reader access without publishing a
// version.
type DraftAccessRequest struct {
	Mode     string   `json:"mode,omitempty"`
	Password string   `json:"password,omitempty"`
	Emails   []string `json:"emails,omitempty"`
}

type DraftAccessResponse struct {
	DraftID      string   `json:"draftId"`
	AccessMode   string   `json:"accessMode"`
	AccessEmails []string `json:"accessEmails"`
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
	AccessMode    string   `json:"accessMode"`
	ContentKind   string   `json:"contentKind"`
	FileCount     int      `json:"fileCount"`
}

// SiteFile is a bundle file to stream in a site upload.
type SiteFile struct {
	Path     string
	Absolute string
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
	AccessMode          string  `json:"accessMode"`
	ContentKind         string  `json:"contentKind"`
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

// UpdateDraftAccess changes a draft's reader access without creating a new
// version.
func (c *Client) UpdateDraftAccess(ctx context.Context, draftID string, request DraftAccessRequest) (DraftAccessResponse, error) {
	var response DraftAccessResponse
	err := c.do(ctx, http.MethodPost, "/api/drafts/"+url.PathEscape(draftID)+"/access", request, &response)
	return response, err
}

// SignOutDraftReaders invalidates every reader session and pending magic link.
func (c *Client) SignOutDraftReaders(ctx context.Context, draftID string) error {
	var response struct{}
	return c.do(ctx, http.MethodPost, "/api/drafts/"+url.PathEscape(draftID)+"/sign-out-readers", nil, &response)
}

// UploadSite publishes a multi-file static site. The multipart body carries a
// `metadata` JSON field (the upload options plus the file paths) followed by one
// `file` part per path, in the same order. The body is streamed from disk.
func (c *Client) UploadSite(ctx context.Context, request UploadRequest, files []SiteFile) (UploadResponse, error) {
	request.HTML, request.Filename, request.Files = "", "", make([]string, len(files))
	for index, file := range files {
		request.Files[index] = file.Path
	}
	metadata, err := json.Marshal(request)
	if err != nil {
		return UploadResponse{}, err
	}
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		writer.CloseWithError(writeSiteForm(form, metadata, files))
	}()
	var response UploadResponse
	err = c.send(ctx, http.MethodPost, "/api/uploads", reader, form.FormDataContentType(), 10*time.Minute, &response)
	_ = reader.Close()
	return response, err
}

func writeSiteForm(form *multipart.Writer, metadata []byte, files []SiteFile) error {
	if err := form.WriteField("metadata", string(metadata)); err != nil {
		return err
	}
	for _, file := range files {
		part, err := form.CreateFormFile("file", path.Base(file.Path))
		if err != nil {
			return err
		}
		source, err := os.Open(file.Absolute)
		if err != nil {
			return err
		}
		_, err = io.Copy(part, source)
		source.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", file.Absolute, err)
		}
	}
	return form.Close()
}

func (c *Client) Drafts(ctx context.Context) ([]Draft, error) {
	var response struct {
		Drafts []Draft `json:"drafts"`
	}
	err := c.do(ctx, http.MethodGet, "/api/drafts", nil, &response)
	return response.Drafts, err
}

type APIKey struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
	Current    bool    `json:"current"`
}

type CreatedAPIKey struct {
	APIKey APIKey `json:"apiKey"`
	Token  string `json:"token"`
}

func (c *Client) APIKeys(ctx context.Context) ([]APIKey, error) {
	var response struct {
		APIKeys []APIKey `json:"apiKeys"`
	}
	err := c.do(ctx, http.MethodGet, "/api/api-keys", nil, &response)
	return response.APIKeys, err
}

func (c *Client) CreateAPIKey(ctx context.Context, name string) (CreatedAPIKey, error) {
	var response CreatedAPIKey
	err := c.do(ctx, http.MethodPost, "/api/api-keys", map[string]string{"name": name}, &response)
	return response, err
}

func (c *Client) RenameAPIKey(ctx context.Context, id, name string) error {
	var response struct{}
	return c.do(ctx, http.MethodPost, "/api/api-keys/"+url.PathEscape(id)+"/rename", map[string]string{"name": name}, &response)
}

func (c *Client) RevokeAPIKey(ctx context.Context, id string) error {
	var response struct{}
	return c.do(ctx, http.MethodPost, "/api/api-keys/"+url.PathEscape(id)+"/revoke", map[string]string{}, &response)
}

func (c *Client) do(ctx context.Context, method, path string, body, target any) error {
	if body == nil {
		return c.send(ctx, method, path, nil, "", 30*time.Second, target)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.send(ctx, method, path, bytes.NewReader(data), "application/json", 30*time.Second, target)
}

func (c *Client) send(ctx context.Context, method, path string, reader io.Reader, contentType string, timeout time.Duration, target any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.Origin+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("User-Agent", "pageferry/"+c.Version)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
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

func (c *Client) ExchangeLogin(ctx context.Context, code, verifier, redirectURI string) (string, error) {
	var response struct {
		Token string `json:"token"`
	}
	err := c.do(ctx, http.MethodPost, "/cli/auth/token", map[string]string{"code": code, "verifier": verifier, "redirectUri": redirectURI}, &response)
	if err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", errors.New("login exchange returned no API key")
	}
	return response.Token, nil
}
