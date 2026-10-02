package command

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gallez-tech/pageferry-cli/internal/state"
)

func loginRandom() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func openLoginBrowser(target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", target)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.CommandContext(ctx, "xdg-open", target)
	}
	return command.Run()
}

func (a *App) browserLogin(ctx context.Context, origin, name string) error {
	name, err := loginKeyName(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	verifier, err := loginRandom()
	if err != nil {
		return err
	}
	stateToken, err := loginRandom()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start login callback: %w", err)
	}
	defer listener.Close()
	redirectURI := "http://" + listener.Addr().String() + "/callback"
	callbackURL, _ := url.Parse(redirectURI)
	code := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		if r.Method != http.MethodGet || r.Host != callbackURL.Host || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(stateToken)) != 1 || r.URL.Query().Get("code") == "" {
			http.Error(w, "Invalid login callback.", http.StatusBadRequest)
			return
		}
		select {
		case code <- r.URL.Query().Get("code"):
			fmt.Fprintln(w, "Authorization received. Return to your terminal to check login completed.")
		default:
			http.Error(w, "Login already received.", http.StatusConflict)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverError := make(chan error, 1)
	go func() { serverError <- server.Serve(listener) }()
	defer server.Close()
	digest := sha256.Sum256([]byte(verifier))
	params := url.Values{"redirect_uri": {redirectURI}, "state": {stateToken}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "name": {name}}
	target := origin + "/cli/auth/authorize?" + params.Encode()
	fmt.Fprintf(a.out, "Opening PageFerry login. If the browser does not open, visit this URL on this computer:\n%s\n\nWaiting for authorization…\n", target)
	openBrowser := a.openBrowser
	if openBrowser == nil {
		openBrowser = openLoginBrowser
	}
	if err := openBrowser(target); err != nil {
		fmt.Fprintln(a.errOut, "Could not open the browser; open the URL above manually.")
	}
	var authCode string
	select {
	case authCode = <-code:
	case err := <-serverError:
		return fmt.Errorf("login callback stopped: %w", err)
	case <-ctx.Done():
		return fmt.Errorf("login cancelled or timed out; credentials were not changed: %w", ctx.Err())
	}
	return a.finishLogin(ctx, origin, authCode, verifier, redirectURI)
}

func (a *App) finishLogin(ctx context.Context, origin, authCode, verifier, redirectURI string) error {
	key, err := a.client(origin, "").ExchangeLogin(ctx, authCode, verifier, redirectURI)
	if err != nil {
		return fmt.Errorf("complete browser login: %w", err)
	}
	identity, err := a.client(origin, key).Me(ctx)
	if err != nil {
		return fmt.Errorf("validate API key: %w", err)
	}
	// Persist the resolved origin, including environment overrides, with its key.
	if err := a.store.SaveConfig(state.Config{APIURL: origin}); err != nil {
		return fmt.Errorf("save API URL: %w", err)
	}
	if err := a.store.SaveCredentials(state.Credentials{APIKey: key, UpdatedAt: a.now().UTC()}); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	fmt.Fprintf(a.out, "Signed in as %s with key %s.\n", identity.AccountName, identity.APIKeyName)
	return nil
}

func loginKeyName(name string) (string, error) {
	if name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return "", fmt.Errorf("read computer hostname: %w", err)
		}
		name = hostname
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return "", errors.New("key name must contain 1 to 80 characters; use --name")
	}
	return name, nil
}
