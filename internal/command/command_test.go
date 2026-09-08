package command

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestValidateRunsOfflineAndUsesPublicFilename(t *testing.T) {
	app, out, errOut := testApp(t, "")
	file := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(file, []byte("<!doctype html><title>Report</title><p>hello</p>"), 0o644); err != nil {
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
	if !strings.Contains(errOut.String(), "No <title> found") {
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
	if got := out.String(); !strings.Contains(got, "pageferry validate <file> [--name <filename>]") {
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
