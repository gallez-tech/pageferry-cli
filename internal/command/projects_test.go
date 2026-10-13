package command

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gallez-tech/pageferry-cli/internal/state"
)

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// projectsApp returns an App signed in against a server that answers every
// request with the given JSON and records what it received.
func projectsApp(t *testing.T, status int, reply any) (*App, *strings.Builder, *[]recordedRequest) {
	t.Helper()
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer pf_saved" {
			t.Errorf("missing authorization")
		}
		recorded := recordedRequest{Method: request.Method, Path: request.URL.Path}
		if data, _ := io.ReadAll(request.Body); len(data) > 0 {
			_ = json.Unmarshal(data, &recorded.Body)
		}
		requests = append(requests, recorded)
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(status)
		_ = json.NewEncoder(response).Encode(reply)
	}))
	t.Cleanup(server.Close)
	app, out, _ := testApp(t, "")
	if err := app.store.SaveConfig(state.Config{APIURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveCredentials(state.Credentials{APIKey: "pf_saved"}); err != nil {
		t.Fatal(err)
	}
	builder := &strings.Builder{}
	app.out = writerFunc(func(p []byte) (int, error) { builder.Write(p); _ = out; return len(p), nil })
	return app, builder, &requests
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestProjectCreateSendsFields(t *testing.T) {
	app, out, requests := projectsApp(t, 201, map[string]any{"ok": true, "project": map[string]any{"slug": "cdg57-concours", "name": "Concours"}})
	err := app.Run(context.Background(), []string{"project", "create", "Concours", "--slug", "cdg57-concours", "--repo", "cdg57/concours", "--description", "Annual contest"})
	if err != nil {
		t.Fatal(err)
	}
	got := (*requests)[0]
	if got.Method != "POST" || got.Path != "/api/projects" {
		t.Fatalf("request = %#v", got)
	}
	if got.Body["name"] != "Concours" || got.Body["slug"] != "cdg57-concours" || got.Body["repo"] != "cdg57/concours" || got.Body["description"] != "Annual contest" {
		t.Fatalf("body = %#v", got.Body)
	}
	if !strings.Contains(out.String(), "--project cdg57-concours") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestProjectCreateFromGitNeedsARemote(t *testing.T) {
	app, _, requests := projectsApp(t, 201, map[string]any{})
	directory := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	err := app.Run(context.Background(), []string{"project", "create", "Concours", "--from-git"})
	if err == nil || !strings.Contains(err.Error(), "origin remote") {
		t.Fatalf("err = %v", err)
	}
	if len(*requests) != 0 {
		t.Fatalf("unexpected requests: %#v", *requests)
	}
	if err := app.Run(context.Background(), []string{"project", "create", "Concours", "--from-git", "--repo", "a/b"}); err == nil {
		t.Fatal("--from-git with --repo was accepted")
	}
}

func TestProjectEditKeepsOmittedFieldsAndClearsRepo(t *testing.T) {
	app, _, requests := projectsApp(t, 200, map[string]any{"ok": true, "project": map[string]any{"slug": "p", "name": "P"}})
	if err := app.Run(context.Background(), []string{"project", "edit", "p", "--name", "New"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"project", "edit", "p", "--no-repo"}); err != nil {
		t.Fatal(err)
	}
	first, second := (*requests)[0], (*requests)[1]
	if first.Method != "PATCH" || first.Path != "/api/projects/p" || first.Body["name"] != "New" {
		t.Fatalf("first = %#v", first)
	}
	if _, present := first.Body["repo"]; present {
		t.Fatalf("repo sent without being asked: %#v", first.Body)
	}
	if repo, present := second.Body["repo"]; !present || repo != "" {
		t.Fatalf("second = %#v", second.Body)
	}
	if err := app.Run(context.Background(), []string{"project", "edit", "p"}); err == nil {
		t.Fatal("empty edit was accepted")
	}
}

func TestProjectRenameDeleteAndMove(t *testing.T) {
	app, out, requests := projectsApp(t, 200, map[string]any{"ok": true, "project": map[string]any{"slug": "p", "name": "Renamed"}})
	if err := app.Run(context.Background(), []string{"project", "rename", "p", "Renamed"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"project", "delete", "p"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"project", "move", "abcdefghijkl", "p"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"project", "move", "abcdefghijkl", "--none"}); err != nil {
		t.Fatal(err)
	}
	methods := []string{}
	for _, request := range *requests {
		methods = append(methods, request.Method+" "+request.Path)
	}
	want := "PATCH /api/projects/p,DELETE /api/projects/p,POST /api/drafts/abcdefghijkl/project,POST /api/drafts/abcdefghijkl/project"
	if strings.Join(methods, ",") != want {
		t.Fatalf("requests = %v", methods)
	}
	if project, present := (*requests)[2].Body["project"]; !present || project != "p" {
		t.Fatalf("move body = %#v", (*requests)[2].Body)
	}
	if project, present := (*requests)[3].Body["project"]; !present || project != nil {
		t.Fatalf("--none body = %#v", (*requests)[3].Body)
	}
	if !strings.Contains(out.String(), "unassigned") {
		t.Fatalf("output = %q", out.String())
	}
	if err := app.Run(context.Background(), []string{"project", "move", "abcdefghijkl"}); err == nil {
		t.Fatal("move without a target was accepted")
	}
}

func TestProjectListShowsCounts(t *testing.T) {
	app, out, _ := projectsApp(t, 200, map[string]any{"projects": []map[string]any{
		{"slug": "cdg57-concours", "name": "Concours", "pageCount": 2, "assetCount": 1, "repoUrl": "https://github.com/cdg57/concours", "updatedAt": "2026-09-03T11:00:00.000Z"},
		{"slug": "admin", "name": "Admin", "pageCount": 0, "assetCount": 0, "updatedAt": "2026-09-03T11:00:00.000Z"},
	}})
	if err := app.Run(context.Background(), []string{"project", "list"}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"Concours — cdg57-concours", "2 pages · 1 asset", "https://github.com/cdg57/concours", "0 pages · 0 assets · no repo"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestUploadSendsProjectAndReportsIt(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&body)
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"draftId": "draft123", "versionNumber": 1, "publicUrl": "https://p-draft123.rgf.sh/report.html",
			"rawUrl": "https://p-draft123.rgf.sh/raw", "hostingMode": "domain", "group": "page",
			"projectSlug": "site-internet", "projectCreated": true,
		})
	}))
	defer server.Close()
	app, out, _ := testApp(t, "")
	_ = app.store.SaveConfig(state.Config{APIURL: server.URL})
	_ = app.store.SaveCredentials(state.Credentials{APIKey: "pf_saved"})
	file := filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(file, []byte("<!doctype html><title>Report</title><p>hello</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"upload", file, "--project", " Site-Internet "}); err != nil {
		t.Fatal(err)
	}
	if body["project"] != "site-internet" {
		t.Fatalf("project = %#v", body["project"])
	}
	if !strings.Contains(out.String(), "Project: site-internet (created)") {
		t.Fatalf("output = %q", out.String())
	}
	if err := app.Run(context.Background(), []string{"upload", file, "--project", " "}); err == nil {
		t.Fatal("blank --project was accepted")
	}
}

func TestListFiltersByGroupProjectAndType(t *testing.T) {
	slug := func(value string) any { return value }
	drafts := []map[string]any{
		{"draftId": "page00000001", "title": "Rules", "group": "page", "type": "html", "projectSlug": slug("concours"), "projectName": "Concours", "versionCount": 1, "publicUrl": "https://x/1", "updatedAt": "2026-09-03T11:00:00.000Z", "accessMode": "public", "hostingMode": "domain"},
		{"draftId": "asset0000001", "title": "Poster", "group": "asset", "type": "image", "sizeBytes": 2048, "projectSlug": slug("concours"), "projectName": "Concours", "versionCount": 1, "publicUrl": "https://x/2", "updatedAt": "2026-09-03T11:00:00.000Z", "accessMode": "public", "hostingMode": "domain"},
		{"draftId": "asset0000002", "title": "Manual", "group": "asset", "type": "pdf", "versionCount": 1, "publicUrl": "https://x/3", "updatedAt": "2026-09-03T11:00:00.000Z", "accessMode": "password", "hostingMode": "domain"},
	}
	run := func(args ...string) string {
		app, out, _ := projectsApp(t, 200, map[string]any{"drafts": drafts})
		if err := app.Run(context.Background(), append([]string{"list"}, args...)); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if text := run("--assets"); strings.Contains(text, "Rules") || !strings.Contains(text, "Poster") || !strings.Contains(text, "2.0 KiB") {
		t.Fatalf("--assets:\n%s", text)
	}
	if text := run("--pages"); !strings.Contains(text, "Rules") || strings.Contains(text, "Poster") {
		t.Fatalf("--pages:\n%s", text)
	}
	if text := run("--project", "none"); !strings.Contains(text, "Manual — no project") || strings.Contains(text, "Poster") {
		t.Fatalf("--project none:\n%s", text)
	}
	if text := run("--type", "pdf"); !strings.Contains(text, "password access") || strings.Contains(text, "Rules") {
		t.Fatalf("--type pdf:\n%s", text)
	}
	if text := run("--project", "concours", "--type", "video"); !strings.Contains(text, "No page or asset matches") {
		t.Fatalf("no match:\n%s", text)
	}
	app, _, _ := projectsApp(t, 200, map[string]any{"drafts": drafts})
	if err := app.Run(context.Background(), []string{"list", "--pages", "--assets"}); err == nil {
		t.Fatal("--pages with --assets was accepted")
	}
}
