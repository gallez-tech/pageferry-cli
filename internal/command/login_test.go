package command

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowserLoginSavesKeyAndResolvedHost(t *testing.T) {
	for _, name := range []string{"", "Work laptop"} {
		t.Run(name, func(t *testing.T) {
			var authorization *url.URL
			expectedName := name
			if expectedName == "" {
				expectedName, _ = os.Hostname()
			}
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/cli/auth/token":
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					digest := sha256.Sum256([]byte(body["verifier"]))
					if body["code"] != "test-code" || body["redirectUri"] != authorization.Query().Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(digest[:]) != authorization.Query().Get("code_challenge") {
						t.Error("invalid code exchange")
					}
					fmt.Fprint(w, `{"token":"pf_browser"}`)
				case "/api/me":
					if r.Header.Get("Authorization") != "Bearer pf_browser" {
						t.Error("missing new key")
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"accountName": "My account", "apiKeyName": expectedName})
				default:
					t.Error("unexpected path: " + r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer apiServer.Close()
			app, out, _ := testApp(t, "")
			app.getenv = func(key string) string {
				if key == "PAGEFERRY_API_URL" {
					return apiServer.URL
				}
				return ""
			}
			app.openBrowser = func(target string) error {
				var err error
				authorization, err = url.Parse(target)
				if err != nil {
					return err
				}
				if authorization.Host != strings.TrimPrefix(apiServer.URL, "http://") || authorization.Query().Get("name") != expectedName {
					t.Error("wrong browser destination or key name")
				}
				callback, _ := url.Parse(authorization.Query().Get("redirect_uri"))
				params := url.Values{"code": {"test-code"}, "state": {"incorrect-state"}}
				callback.RawQuery = params.Encode()
				bad, err := http.Get(callback.String())
				if err != nil {
					return err
				}
				bad.Body.Close()
				if bad.StatusCode != 400 {
					t.Error("invalid state accepted")
				}
				params.Set("state", authorization.Query().Get("state"))
				callback.RawQuery = params.Encode()
				response, err := http.Get(callback.String())
				if err != nil {
					return err
				}
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Error("callback failed")
				}
				return nil
			}
			args := []string{"auth", "login"}
			if name != "" {
				args = append(args, "--name", name)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := app.Run(ctx, args); err != nil {
				t.Fatal(err)
			}
			if app.store.LoadCredentials().APIKey != "pf_browser" || app.store.LoadConfig().APIURL != apiServer.URL {
				t.Fatal("credentials or host not saved")
			}
			if !strings.Contains(out.String(), "Signed in as My account") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestBrowserLoginCancellationPreservesCredentials(t *testing.T) {
	app, _, _ := testApp(t, "")
	if err := app.Run(context.Background(), []string{"auth", "set", "pf_previous"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.openBrowser = func(string) error { cancel(); return nil }
	if err := app.Run(ctx, []string{"auth", "login"}); err == nil {
		t.Fatal("expected cancellation")
	}
	if app.store.LoadCredentials().APIKey != "pf_previous" {
		t.Fatal("credentials changed")
	}
}

func TestBrowserLoginAcceptsCallbackPastedFromAnotherDevice(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cli/auth/token" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != "pasted-code" {
				t.Error("pasted code not exchanged")
			}
			fmt.Fprint(w, `{"token":"pf_pasted"}`)
			return
		}
		fmt.Fprint(w, `{"accountName":"Owner","apiKeyName":"seedbox"}`)
	}))
	defer apiServer.Close()
	app, out, errOut := testApp(t, "")
	reader, writer := io.Pipe()
	app.in = reader
	app.openBrowser = func(target string) error {
		authorization, err := url.Parse(target)
		if err != nil {
			return err
		}
		callback, _ := url.Parse(authorization.Query().Get("redirect_uri"))
		go func() {
			wrong := *callback
			wrong.RawQuery = url.Values{"code": {"x"}, "state": {"another-login"}}.Encode()
			fmt.Fprintln(writer, wrong.String())
			callback.RawQuery = url.Values{"code": {"pasted-code"}, "state": {authorization.Query().Get("state")}}.Encode()
			fmt.Fprintln(writer, callback.String())
		}()
		return errors.New("no browser on this host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx, []string{"auth", "login", "--api-url", apiServer.URL}); err != nil {
		t.Fatal(err)
	}
	if app.store.LoadCredentials().APIKey != "pf_pasted" {
		t.Fatal("pasted callback did not save the key")
	}
	if !strings.Contains(errOut.String(), "not the callback address for this login") || !strings.Contains(out.String(), "paste it here") {
		t.Fatalf("missing guidance: %s / %s", out.String(), errOut.String())
	}
}

func TestLoginUsesLoginCodeOverSSHUnlessBrowserIsForced(t *testing.T) {
	app, out, errOut := testApp(t, "")
	app.getenv = func(key string) string {
		if key == "SSH_CONNECTION" {
			return "203.0.113.5 52000 198.51.100.7 22"
		}
		return ""
	}
	app.openBrowser = func(string) error { t.Error("SSH login opened a browser"); return nil }
	if err := app.Run(context.Background(), []string{"auth", "login", "--api-url", "https://p.example"}); err == nil {
		t.Fatal("expected an empty login code to fail")
	}
	if !strings.Contains(errOut.String(), "SSH session detected") || !strings.Contains(out.String(), "/cli/auth/authorize?") ||
		!strings.Contains(out.String(), url.QueryEscape("urn:pageferry:cli:headless")) {
		t.Fatalf("expected a login-code flow: %s / %s", out.String(), errOut.String())
	}

	opened := false
	ctx, cancel := context.WithCancel(context.Background())
	app.openBrowser = func(string) error { opened = true; cancel(); return nil }
	_ = app.Run(ctx, []string{"auth", "login", "--browser", "--api-url", "https://p.example"})
	if !opened {
		t.Fatal("--browser did not open a browser")
	}
	if err := app.Run(context.Background(), []string{"auth", "login", "--browser", "--headless"}); err == nil {
		t.Fatal("conflicting login modes accepted")
	}
}
