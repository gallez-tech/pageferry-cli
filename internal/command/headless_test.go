package command

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentLoginAcrossCommandsKeepsSecretsOutOfOutput(t *testing.T) {
	app, out, errOut := testApp(t, "")
	code := strings.Repeat("c", 43)
	verifier := ""
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cli/auth/token":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["code"] != code || body["verifier"] != verifier || body["redirectUri"] != headlessRedirectURI {
				t.Error("incorrect headless exchange")
			}
			fmt.Fprint(w, `{"token":"pf_agent_secret"}`)
		case "/api/me":
			if r.Header.Get("Authorization") != "Bearer pf_agent_secret" {
				t.Error("missing API key")
			}
			fmt.Fprint(w, `{"accountName":"Owner","apiKeyName":"VPS agent"}`)
		default:
			t.Error("unexpected path")
			w.WriteHeader(404)
		}
	}))
	defer apiServer.Close()
	if err := app.Run(context.Background(), []string{"auth", "start", "--name", "VPS agent", "--api-url", apiServer.URL}); err != nil {
		t.Fatal(err)
	}
	pending := app.store.LoadPendingLogin()
	verifier = pending.Verifier
	if verifier == "" || app.store.LoadCredentials().APIKey != "" {
		t.Fatal("start should save only the pending login")
	}
	info, err := os.Stat(filepath.Join(app.store.Dir, "pending-login.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("pending login is not private")
	}
	var target *url.URL
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, apiServer.URL+"/cli/auth/authorize?") {
			target, _ = url.Parse(line)
		}
	}
	digest := sha256.Sum256([]byte(verifier))
	if target == nil || target.Query().Get("name") != "VPS agent" || target.Query().Get("redirect_uri") != headlessRedirectURI || target.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatal("bad authorization link")
	}
	complete, completeOut, completeErr := testApp(t, "pf_login_"+code+"."+pending.State+"\n")
	complete.store = app.store
	// An unrelated environment override cannot send the exchange to another host.
	complete.getenv = func(string) string { return "https://other.example" }
	if err := complete.Run(context.Background(), []string{"auth", "complete", "--stdin"}); err != nil {
		t.Fatal(err)
	}
	if complete.store.LoadCredentials().APIKey != "pf_agent_secret" || complete.store.LoadConfig().APIURL != apiServer.URL {
		t.Fatal("credentials not saved")
	}
	if complete.store.LoadPendingLogin().Verifier != "" {
		t.Fatal("pending verifier was not deleted")
	}
	output := out.String() + errOut.String() + completeOut.String() + completeErr.String()
	if strings.Contains(output, "pf_agent_secret") || strings.Contains(output, verifier) {
		t.Fatal("secret appeared in output")
	}
	if err := complete.Run(context.Background(), []string{"auth", "complete", "pf_login_" + code + "." + pending.State}); err == nil {
		t.Fatal("completed request reused")
	}
}

func TestHeadlessLoginRejectsWrongAndExpiredCodes(t *testing.T) {
	app, _, _ := testApp(t, "")
	if err := app.Run(context.Background(), []string{"auth", "set", "pf_previous"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"auth", "start"}); err != nil {
		t.Fatal(err)
	}
	pending := app.store.LoadPendingLogin()
	for _, code := range []string{"", "pf_login_" + strings.Repeat("c", 43) + "." + strings.Repeat("x", 43)} {
		if err := app.Run(context.Background(), []string{"auth", "complete", code}); err == nil {
			t.Fatal("invalid code accepted")
		}
	}
	app.now = func() time.Time { return pending.ExpiresAt }
	if err := app.Run(context.Background(), []string{"auth", "complete", "pf_login_" + strings.Repeat("c", 43) + "." + pending.State}); err == nil {
		t.Fatal("expired request accepted")
	}
	if app.store.LoadCredentials().APIKey != "pf_previous" {
		t.Fatal("credentials changed")
	}
	if app.store.LoadPendingLogin().Verifier != "" {
		t.Fatal("expired verifier retained")
	}
	if err := app.Run(context.Background(), []string{"auth", "login", "--headless", "--manual"}); err == nil {
		t.Fatal("conflicting modes accepted")
	}
}

type loginCodeReader func([]byte) (int, error)

func (reader loginCodeReader) Read(p []byte) (int, error) { return reader(p) }

func TestInteractiveHeadlessLogin(t *testing.T) {
	app, out, _ := testApp(t, "")
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cli/auth/token" {
			fmt.Fprint(w, `{"token":"pf_remote_secret"}`)
		} else {
			fmt.Fprint(w, `{"accountName":"Owner","apiKeyName":"VPS"}`)
		}
	}))
	defer apiServer.Close()
	app.openBrowser = func(string) error { t.Error("headless login opened a browser"); return nil }
	app.in = loginCodeReader(func(p []byte) (int, error) {
		code := "pf_login_" + strings.Repeat("c", 43) + "." + app.store.LoadPendingLogin().State + "\n"
		return copy(p, code), io.EOF
	})
	if err := app.Run(context.Background(), []string{"auth", "login", "--headless", "--name", "VPS", "--api-url", apiServer.URL}); err != nil {
		t.Fatal(err)
	}
	if app.store.LoadCredentials().APIKey != "pf_remote_secret" || strings.Contains(out.String(), "pf_remote_secret") {
		t.Fatal("API key missing or disclosed")
	}
}

func TestHeadlessLoginCancellationPreservesCredentials(t *testing.T) {
	app, _, _ := testApp(t, "")
	if err := app.Run(context.Background(), []string{"auth", "set", "pf_previous"}); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	app.in = reader
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx, []string{"auth", "login", "--headless"}); err == nil {
		t.Fatal("expected cancellation")
	}
	if app.store.LoadCredentials().APIKey != "pf_previous" {
		t.Fatal("credentials changed")
	}
}
