package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gallez-tech/pageferry-cli/internal/api"
	"github.com/gallez-tech/pageferry-cli/internal/state"
)

func testApp(t *testing.T, in string) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{
		version: "test-version",
		in:      strings.NewReader(in),
		out:     out,
		errOut:  errOut,
		store:   state.New(filepath.Join(t.TempDir(), "state")),
		now:     func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) },
		getenv:  func(string) string { return "" },
	}
	return app, out, errOut
}

func TestUploadCreatesThenUsesSavedMapping(t *testing.T) {
	var requests []api.UploadRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer pf_saved" {
			t.Errorf("missing authorization")
		}
		var body api.UploadRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, body)
		version := len(requests)
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"draftId": "draft123", "versionNumber": version,
			"publicUrl":   "https://p-draft123.rgf.sh/report.html",
			"rawUrl":      "https://p-draft123.rgf.sh/raw",
			"versionUrl":  "https://p-draft123.rgf.sh/v/1/report.html",
			"hostingMode": "domain", "warnings": []string{},
		})
	}))
	defer server.Close()
	app, out, _ := testApp(t, "")
	if err := app.store.SaveConfig(state.Config{APIURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveCredentials(state.Credentials{APIKey: "pf_saved"}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(file, []byte("<!doctype html><title>Report</title><p>hello</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := app.Run(context.Background(), []string{"upload", file}); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 2 || requests[0].DraftID != "" || requests[1].DraftID != "draft123" {
		t.Fatalf("requests = %#v", requests)
	}
	if requests[0].Metadata["cliVersion"] != "test-version" || requests[0].Metadata["contentSha256"] == "" {
		t.Fatalf("metadata = %#v", requests[0].Metadata)
	}
	if !strings.Contains(out.String(), "Uploaded draft") || !strings.Contains(out.String(), "Updated draft") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestUploadRequiresKeyBeforeReading(t *testing.T) {
	app, _, _ := testApp(t, "")
	file := filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(file, []byte("not html"), 0o000); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{"upload", file})
	if err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("error = %v", err)
	}
}

func TestUploadSendsPrivateAccessAndBackendVariables(t *testing.T) {
	var uploaded api.UploadRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&uploaded); err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"draftId": "draft123", "versionNumber": 1, "publicUrl": "https://p-draft123.rgf.sh/page.html", "rawUrl": "https://p-draft123.rgf.sh/raw", "versionUrl": "https://p-draft123.rgf.sh/v/1/page.html", "hostingMode": "domain", "accessMode": "password", "warnings": []string{}})
	}))
	defer server.Close()
	app, _, _ := testApp(t, "")
	_ = app.store.SaveConfig(state.Config{APIURL: server.URL})
	_ = app.store.SaveCredentials(state.Credentials{APIKey: "pf_saved"})
	file := filepath.Join(t.TempDir(), "page.html")
	_ = os.WriteFile(file, []byte("<!doctype html><title>Private</title>"), 0o644)
	err := app.Run(context.Background(), []string{"upload", file, "--password", "long-password", "--env", "WEBHOOK_URL=https://example.com/hook", "--secret", "API_KEY=hidden", "--env", "MODE=test"})
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Access == nil || uploaded.Access.Password != "long-password" || uploaded.Env["MODE"] != "test" || uploaded.Secrets["API_KEY"] != "hidden" {
		t.Fatalf("upload = %#v", uploaded)
	}
}

func TestValidateRunsOfflineAndUsesPublicFilename(t *testing.T) {
	app, out, errOut := testApp(t, "")
	file := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(file, []byte(`<!doctype html><html lang="en"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Report</title><p>hello</p>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"validate", file, "--name", "report.html"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Valid PageFerry document:") || !strings.Contains(out.String(), "public filename: report.html") {
		t.Fatalf("output = %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestValidateReportsWarningsAndPolicyErrors(t *testing.T) {
	app, _, errOut := testApp(t, "")
	file := filepath.Join(t.TempDir(), "unsafe.html")
	if err := os.WriteFile(file, []byte(`<iframe src="https://example.com"></iframe>`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{"validate", file})
	if err == nil || !strings.Contains(err.Error(), "Blocked <iframe> tag found.") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(errOut.String(), "No <title> found") || !strings.Contains(errOut.String(), `No <meta name="viewport"> found`) {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestValidateRejectsInvalidPublicFilename(t *testing.T) {
	app, _, _ := testApp(t, "")
	file := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(file, []byte("<!doctype html><title>Report</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{"validate", file})
	if err == nil || !strings.Contains(err.Error(), "Filename must end with .html or .htm.") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateHelpIsLocalAndDocumentsName(t *testing.T) {
	app, out, _ := testApp(t, "")
	if err := app.Run(context.Background(), []string{"validate", "--help"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "pageferry validate <file|directory> [--name <filename>]") {
		t.Fatalf("help = %q", got)
	}
}

func TestLoginDoesNotReplaceCredentialsOnEmptyInput(t *testing.T) {
	app, _, _ := testApp(t, "")
	if err := app.store.SaveCredentials(state.Credentials{APIKey: "existing"}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{"auth", "login"})
	if err == nil || app.store.LoadCredentials().APIKey != "existing" {
		t.Fatalf("error = %v credentials = %#v", err, app.store.LoadCredentials())
	}
}

func TestParseDurationBounds(t *testing.T) {
	for _, valid := range []string{"5m", "24h", "7d", "30d", "720h"} {
		if _, err := parseDuration(valid); err != nil {
			t.Errorf("%s: %v", valid, err)
		}
	}
	for _, invalid := range []string{"4m59s", "721h", "31d", "1w", "1.5s"} {
		if _, err := parseDuration(invalid); err == nil {
			t.Errorf("%s was accepted", invalid)
		}
	}
}

const slidevIndex = `<!doctype html><html><head><title>Deck</title>
<script type="module" crossorigin src="/assets/index-DkT9yQ3a.js"></script>
<link rel="stylesheet" href="/assets/index-B8f2Kq1L.css"></head><body><div id="app"></div></body></html>`

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUploadDirectoryBuildsAndSendsSiteBundle(t *testing.T) {
	var metadata api.UploadRequest
	received := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("content type = %q", request.Header.Get("Content-Type"))
		}
		reader, err := request.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		var parts []string
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			if part.FormName() == "metadata" {
				_ = json.Unmarshal(data, &metadata)
			} else {
				parts = append(parts, string(data))
			}
		}
		for index, path := range metadata.Files {
			received[path] = parts[index]
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"draftId": "site12345678", "versionNumber": 1, "publicUrl": "https://p-site12345678.rgf.sh/", "versionUrl": "https://p-site12345678.rgf.sh/v/1/", "hostingMode": "domain", "contentKind": "site", "fileCount": len(parts), "warnings": []string{}})
	}))
	defer server.Close()
	app, out, _ := testApp(t, "")
	_ = app.store.SaveConfig(state.Config{APIURL: server.URL})
	_ = app.store.SaveCredentials(state.Credentials{APIKey: "pf_saved"})
	project := t.TempDir()
	writeFiles(t, project, map[string]string{"slides.md": "# Deck", "package.json": "{}"})
	var builtIn string
	app.build = func(_ context.Context, dir string, _ io.Writer) error {
		builtIn = dir
		writeFiles(t, dir, map[string]string{
			"dist/index.html":                slidevIndex,
			"dist/assets/index-DkT9yQ3a.js":  "console.log(1)",
			"dist/assets/index-B8f2Kq1L.css": "body{}",
			"dist/.DS_Store":                 "junk",
		})
		return nil
	}
	if err := app.Run(context.Background(), []string{"upload", project, "--build"}); err != nil {
		t.Fatal(err)
	}
	if builtIn != project {
		t.Fatalf("build ran in %q", builtIn)
	}
	if metadata.HTML != "" || metadata.Filename != "" || len(metadata.Files) != 3 {
		t.Fatalf("metadata = %#v", metadata)
	}
	if received["index.html"] != slidevIndex || received["assets/index-DkT9yQ3a.js"] != "console.log(1)" {
		t.Fatalf("received = %#v", received)
	}
	if _, ok := received[".DS_Store"]; ok {
		t.Fatal("hidden file was uploaded")
	}
	if !strings.Contains(out.String(), "Public URL: https://p-site12345678.rgf.sh/") || !strings.Contains(out.String(), "Files: 3") {
		t.Fatalf("output = %q", out.String())
	}
	if app.store.LoadDrafts()[project].DraftID != "site12345678" {
		t.Fatal("draft mapping was not saved for the project directory")
	}
}

func TestValidateDirectoryRejectsUnsafeSites(t *testing.T) {
	app, out, _ := testApp(t, "")
	good := t.TempDir()
	writeFiles(t, good, map[string]string{"index.html": slidevIndex, "assets/index-DkT9yQ3a.js": "1"})
	if err := app.Run(context.Background(), []string{"validate", good}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Valid PageFerry site") {
		t.Fatalf("output = %q", out.String())
	}

	missing := t.TempDir()
	writeFiles(t, missing, map[string]string{"slides.md": "# Deck"})
	if err := app.Run(context.Background(), []string{"validate", missing}); err == nil || !strings.Contains(err.Error(), "npm run build") {
		t.Fatalf("error = %v", err)
	}

	unsafe := t.TempDir()
	writeFiles(t, unsafe, map[string]string{"index.html": slidevIndex, "embed.html": "<title>x</title><iframe></iframe>"})
	if err := app.Run(context.Background(), []string{"validate", unsafe}); err == nil || !strings.Contains(err.Error(), "embed.html: Blocked <iframe> tag found.") {
		t.Fatalf("error = %v", err)
	}
}

func TestUploadDirectoryReportsServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		response.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = response.Write([]byte(`{"ok":false,"errors":["Site bundle must contain index.html at its root."]}`))
	}))
	defer server.Close()
	app, _, _ := testApp(t, "")
	_ = app.store.SaveConfig(state.Config{APIURL: server.URL})
	_ = app.store.SaveCredentials(state.Credentials{APIKey: "pf_saved"})
	site := t.TempDir()
	writeFiles(t, site, map[string]string{"index.html": slidevIndex})
	err := app.Run(context.Background(), []string{"upload", site})
	if err == nil || !strings.Contains(err.Error(), "index.html at its root") {
		t.Fatalf("error = %v", err)
	}
	if len(app.store.LoadDrafts()) != 0 {
		t.Fatal("failed upload saved a draft mapping")
	}
}

func keysServer(t *testing.T, renamed *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer pf_saved" {
			http.Error(response, `{"error":"Missing or invalid API key."}`, http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/me":
			_ = json.NewEncoder(response).Encode(map[string]any{"accountId": "acct", "accountName": "Owner", "apiKeyId": "key_1", "apiKeyName": "CLI · 2026-09-03"})
		case request.Method == http.MethodGet && request.URL.Path == "/api/api-keys":
			_ = json.NewEncoder(response).Encode(map[string]any{"apiKeys": []map[string]any{
				{"id": "key_1", "name": "Laptop", "createdAt": "2026-09-01T12:00:00Z", "lastUsedAt": "2026-09-03T11:00:00Z", "current": true},
				{"id": "key_2", "name": "CI", "createdAt": "2026-09-02T12:00:00Z", "lastUsedAt": nil, "current": false},
			}})
		case request.Method == http.MethodPost && request.URL.Path == "/api/api-keys":
			var body map[string]string
			_ = json.NewDecoder(request.Body).Decode(&body)
			response.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(response).Encode(map[string]any{"apiKey": map[string]any{"id": "key_3", "name": body["name"]}, "token": "pf_new"})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/rename"):
			var body map[string]string
			_ = json.NewDecoder(request.Body).Decode(&body)
			(*renamed)[strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/api/api-keys/"), "/rename")] = body["name"]
			_ = json.NewEncoder(response).Encode(map[string]any{"ok": true})
		case request.Method == http.MethodPost && request.URL.Path == "/api/api-keys/key_2/revoke":
			_ = json.NewEncoder(response).Encode(map[string]any{"ok": true})
		default:
			http.Error(response, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
}

func TestKeysCommands(t *testing.T) {
	renamed := map[string]string{}
	server := keysServer(t, &renamed)
	defer server.Close()
	app, out, _ := testApp(t, "")
	if err := app.store.SaveConfig(state.Config{APIURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveCredentials(state.Credentials{APIKey: "pf_saved"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"keys", "list"},
		{"keys", "create", "--name", "Build server"},
		{"keys", "rename", "key_2", "Nightly", "CI"},
		{"keys", "revoke", "key_2"},
	} {
		if err := app.Run(context.Background(), args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	got := out.String()
	for _, want := range []string{"* Laptop — key_1", "used 1h ago", "  CI — key_2", "never used", "Created API key Build server (key_3)", "pf_new", "Renamed API key key_2 to Nightly CI.", "Revoked API key key_2."} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if renamed["key_2"] != "Nightly CI" {
		t.Fatalf("renamed = %#v", renamed)
	}
}

func TestLoginNamesTheKey(t *testing.T) {
	renamed := map[string]string{}
	server := keysServer(t, &renamed)
	defer server.Close()
	app, out, _ := testApp(t, "pf_saved\n")
	if err := app.Run(context.Background(), []string{"auth", "login", "--name", "Work laptop", "--api-url", server.URL}); err != nil {
		t.Fatal(err)
	}
	if renamed["key_1"] != "Work laptop" || !strings.Contains(out.String(), "with key Work laptop.") {
		t.Fatalf("renamed = %#v output = %q", renamed, out.String())
	}
	if app.store.LoadCredentials().APIKey != "pf_saved" {
		t.Fatal("credentials were not saved")
	}
}

func TestAuthSetWithNameRejectsInvalidKeyWithoutSaving(t *testing.T) {
	renamed := map[string]string{}
	server := keysServer(t, &renamed)
	defer server.Close()
	app, _, _ := testApp(t, "")
	err := app.Run(context.Background(), []string{"auth", "set", "pf_wrong", "--name", "Laptop", "--api-url", server.URL})
	if err == nil || app.store.LoadCredentials().APIKey != "" {
		t.Fatalf("error = %v credentials = %#v", err, app.store.LoadCredentials())
	}
}
