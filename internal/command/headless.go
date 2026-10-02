package command

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gallez-tech/pageferry-cli/internal/state"
)

const headlessRedirectURI = "urn:pageferry:cli:headless"

func (a *App) startHeadlessLogin(origin, name string) error {
	name, err := loginKeyName(name)
	if err != nil {
		return err
	}
	verifier, err := loginRandom()
	if err != nil {
		return err
	}
	stateToken, err := loginRandom()
	if err != nil {
		return err
	}
	pending := state.PendingLogin{APIURL: origin, Verifier: verifier, State: stateToken, ExpiresAt: a.now().Add(5 * time.Minute)}
	if err := a.store.SavePendingLogin(pending); err != nil {
		return fmt.Errorf("save pending login: %w", err)
	}
	digest := sha256.Sum256([]byte(verifier))
	params := url.Values{"redirect_uri": {headlessRedirectURI}, "state": {stateToken}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "name": {name}}
	fmt.Fprintf(a.out, "Open this URL on any device and authorize %s:\n%s/cli/auth/authorize?%s\n\nReturn the one-time login code to this terminal. This request expires in five minutes.\n", name, origin, params.Encode())
	return nil
}

func (a *App) authStart(args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry auth start [--name <key-name>] [--api-url <url>]")
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true, "name": true})
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return errors.New("usage: pageferry auth start [--name <key-name>] [--api-url <url>]")
	}
	if name, ok := options["name"]; ok && strings.TrimSpace(name) == "" {
		return errors.New("--name must not be empty")
	}
	origin, err := a.origin(options["api-url"])
	if err != nil {
		return err
	}
	if err := a.startHeadlessLogin(origin, options["name"]); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Complete with: pageferry auth complete <login-code>")
	return nil
}

func (a *App) headlessLogin(ctx context.Context, origin, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := a.startHeadlessLogin(origin, name); err != nil {
		return err
	}
	fmt.Fprint(a.out, "\nPaste one-time login code: ")
	code, err := a.readLoginCode(ctx)
	if err != nil {
		return err
	}
	return a.completeHeadlessLogin(ctx, code)
}

func (a *App) readLoginCode(ctx context.Context) (string, error) {
	result := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(a.in)
		if scanner.Scan() {
			result <- strings.TrimSpace(scanner.Text())
		} else {
			result <- ""
		}
	}()
	select {
	case code := <-result:
		if code == "" {
			return "", errors.New("no login code entered; credentials were not changed")
		}
		return code, nil
	case <-ctx.Done():
		return "", fmt.Errorf("login cancelled or timed out; credentials were not changed: %w", ctx.Err())
	}
}

func (a *App) authComplete(ctx context.Context, args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry auth complete <login-code> | --stdin")
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"stdin": false})
	if err != nil {
		return err
	}
	_, stdin := options["stdin"]
	if (stdin && len(positional) != 0) || (!stdin && len(positional) != 1) {
		return errors.New("usage: pageferry auth complete <login-code> | --stdin")
	}
	code := ""
	if stdin {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		code, err = a.readLoginCode(ctx)
		if err != nil {
			return err
		}
	} else {
		code = positional[0]
	}
	return a.completeHeadlessLogin(ctx, code)
}

func (a *App) completeHeadlessLogin(ctx context.Context, code string) error {
	pending := a.store.LoadPendingLogin()
	if pending.Verifier == "" || pending.State == "" || pending.APIURL == "" {
		return errors.New("no pending login; run pageferry auth start first")
	}
	if !a.now().Before(pending.ExpiresAt) {
		if err := a.store.ClearPendingLogin(); err != nil {
			return fmt.Errorf("clear expired login: %w", err)
		}
		return errors.New("login request expired; run pageferry auth start again")
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(code), "pf_login_"), ".")
	if len(parts) != 2 || len(parts[0]) != 43 || subtle.ConstantTimeCompare([]byte(parts[1]), []byte(pending.State)) != 1 {
		return errors.New("invalid login code or code belongs to another request; credentials were not changed")
	}
	if err := a.finishLogin(ctx, pending.APIURL, parts[0], pending.Verifier, headlessRedirectURI); err != nil {
		return err
	}
	if err := a.store.ClearPendingLogin(); err != nil {
		return fmt.Errorf("signed in but could not clear pending login: %w", err)
	}
	return nil
}
