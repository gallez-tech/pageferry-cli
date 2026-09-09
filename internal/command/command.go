package command

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gallez-tech/pageferry-cli/internal/api"
	"github.com/gallez-tech/pageferry-cli/internal/policy"
	"github.com/gallez-tech/pageferry-cli/internal/provenance"
	"github.com/gallez-tech/pageferry-cli/internal/skill"
	"github.com/gallez-tech/pageferry-cli/internal/state"
	"github.com/gallez-tech/pageferry-cli/internal/update"
)

const defaultAPIURL = "https://p.rgf.sh"

type App struct {
	version string
	in      io.Reader
	out     io.Writer
	errOut  io.Writer
	store   *state.Store
	now     func() time.Time
	getenv  func(string) string
}

func New(version string, in io.Reader, out, errOut io.Writer) (*App, error) {
	store, err := state.Default()
	if err != nil {
		return nil, err
	}
	return &App{version: version, in: in, out: out, errOut: errOut, store: store, now: time.Now, getenv: os.Getenv}, nil
}

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		a.printHelp()
		return nil
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintf(a.out, "pageferry %s\n", a.version)
		return nil
	}
	switch args[0] {
	case "auth":
		return a.auth(ctx, args[1:])
	case "whoami":
		return a.whoami(ctx, args[1:])
	case "upload":
		return a.upload(ctx, args[1:])
	case "validate":
		return a.validate(args[1:])
	case "list":
		return a.list(ctx, args[1:])
	case "skill":
		return a.skill(args[1:])
	case "update":
		return a.update(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q; use --help", args[0])
	}
}

func (a *App) printHelp() {
	fmt.Fprint(a.out, `PageFerry publishes a local HTML document as a stable public URL.

Usage:
  pageferry auth set <api-key> [--api-url <url>]
  pageferry auth login [--api-url <url>]
  pageferry whoami [--api-url <url>]
  pageferry upload <file> [--draft <id>] [--new] [--name <filename>]
                   [--description <text>] [--temporary <duration>]
                   [--public | --password <password> | --email <address>]
                   [--env <NAME=value>] [--secret <NAME=value>]
                   [--workers-dev] [--api-url <url>]
  pageferry validate <file> [--name <filename>]
  pageferry list [--api-url <url>] [--json]
  pageferry skill install --agent <opencode|codex|claude|cursor|all>
                          [--local|--global] [--force]
                          (opencode/codex/cursor → .agents/skills; claude → .claude/skills)
  pageferry update check
  pageferry --version

Environment:
  PAGEFERRY_API_URL  Override the saved API origin.
  PAGEFERRY_API_KEY  Override the saved API key.
`)
}

func (a *App) validate(args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry validate <file> [--name <filename>]")
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"name": true})
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("usage: pageferry validate <file> [--name <filename>]")
	}
	absolute, err := filepath.Abs(positional[0])
	if err != nil {
		return fmt.Errorf("resolve file: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("open %s: %w", absolute, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", absolute)
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return fmt.Errorf("read %s: %w", absolute, err)
	}
	validation := policy.ValidateHTML(content)
	for _, warning := range validation.Warnings {
		fmt.Fprintf(a.errOut, "warning: %s\n", warning)
	}
	if len(validation.Errors) > 0 {
		return fmt.Errorf("HTML validation failed:\n  - %s", strings.Join(validation.Errors, "\n  - "))
	}
	filename := filepath.Base(absolute)
	if suppliedName, supplied := options["name"]; supplied {
		filename = suppliedName
	}
	filename, filenameErrors := policy.ValidateFilename(filename)
	if err := policy.FilenameError(filenameErrors); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Valid PageFerry document: %s (public filename: %s)\n", absolute, filename)
	return nil
}

func (a *App) update(ctx context.Context, args []string) error {
	if len(args) != 1 || (args[0] != "check" && !isHelp(args[0])) {
		return errors.New("usage: pageferry update check")
	}
	if isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry update check")
		return nil
	}
	result, err := update.Check(ctx, a.version, nil)
	if err != nil {
		return err
	}
	if result.Outdated {
		fmt.Fprintf(a.out, "Update available: %s → %s\n%s\n", result.Current, result.Latest, result.ReleaseURL)
		return nil
	}
	if a.version == "dev" {
		fmt.Fprintf(a.out, "Development build; latest release is %s.\n", result.Latest)
		return nil
	}
	fmt.Fprintf(a.out, "PageFerry %s is up to date.\n", result.Current)
	return nil
}

func (a *App) auth(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry auth <set|login> [arguments]")
		return nil
	}
	switch args[0] {
	case "set":
		if len(args) == 2 && isHelp(args[1]) {
			fmt.Fprintln(a.out, "Usage: pageferry auth set <api-key> [--api-url <url>]")
			return nil
		}
		options, positional, err := parseOptions(args[1:], map[string]bool{"api-url": true})
		if err != nil {
			return err
		}
		if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
			return errors.New("usage: pageferry auth set <api-key> [--api-url <url>]")
		}
		if raw, ok := options["api-url"]; ok {
			origin, err := api.ValidateOrigin(raw)
			if err != nil {
				return err
			}
			if err := a.store.SaveConfig(state.Config{APIURL: origin}); err != nil {
				return fmt.Errorf("save API URL: %w", err)
			}
		}
		credentials := state.Credentials{APIKey: strings.TrimSpace(positional[0]), UpdatedAt: a.now().UTC()}
		if err := a.store.SaveCredentials(credentials); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
		fmt.Fprintln(a.out, "Credentials saved.")
		return nil
	case "login":
		if len(args) == 2 && isHelp(args[1]) {
			fmt.Fprintln(a.out, "Usage: pageferry auth login [--api-url <url>]")
			return nil
		}
		options, positional, err := parseOptions(args[1:], map[string]bool{"api-url": true})
		if err != nil {
			return err
		}
		if len(positional) != 0 {
			return errors.New("usage: pageferry auth login [--api-url <url>]")
		}
		origin, err := a.origin(options["api-url"])
		if err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Open this URL on any device:\n%s/cli/auth\n\nPaste API key: ", origin)
		scanner := bufio.NewScanner(a.in)
		if !scanner.Scan() || strings.TrimSpace(scanner.Text()) == "" {
			return errors.New("no API key entered; credentials were not changed")
		}
		key := strings.TrimSpace(scanner.Text())
		identity, err := a.client(origin, key).Me(ctx)
		if err != nil {
			return fmt.Errorf("validate API key: %w", err)
		}
		if _, supplied := options["api-url"]; supplied {
			if err := a.store.SaveConfig(state.Config{APIURL: origin}); err != nil {
				return fmt.Errorf("save API URL: %w", err)
			}
		}
		if err := a.store.SaveCredentials(state.Credentials{APIKey: key, UpdatedAt: a.now().UTC()}); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
		fmt.Fprintf(a.out, "\nSigned in as %s with key %s.\n", identity.AccountName, identity.APIKeyName)
		return nil
	default:
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func (a *App) whoami(ctx context.Context, args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry whoami [--api-url <url>]")
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true})
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return errors.New("usage: pageferry whoami [--api-url <url>]")
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	identity, err := client.Me(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Account: %s (%s)\nAPI key: %s (%s)\n", identity.AccountName, identity.AccountID, identity.APIKeyName, identity.APIKeyID)
	return nil
}

func (a *App) upload(ctx context.Context, args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry upload <file> [--draft <id>] [--new] [--name <filename>] [--description <text>] [--temporary <duration>] [--public | --password <password> | --email <address>] [--env <NAME=value>] [--secret <NAME=value>] [--workers-dev] [--api-url <url>]")
		return nil
	}
	spec := map[string]bool{"api-url": true, "draft": true, "new": false, "name": true, "description": true, "temporary": true, "public": false, "password": true, "email": true, "env": true, "secret": true, "workers-dev": false}
	options, positional, err := parseOptions(args, spec)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("usage: pageferry upload <file> [options]")
	}
	if _, newDraft := options["new"]; newDraft && options["draft"] != "" {
		return errors.New("--new and --draft cannot be used together")
	}
	if _, workersDev := options["workers-dev"]; workersDev && options["temporary"] == "" {
		return errors.New("--workers-dev requires --temporary")
	}
	_, makePublic := options["public"]
	if boolCount(makePublic, options["password"] != "", options["email"] != "") > 1 {
		return errors.New("--public, --password, and --email are mutually exclusive")
	}
	absolute, err := filepath.Abs(positional[0])
	if err != nil {
		return fmt.Errorf("resolve file: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("open %s: %w", absolute, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", absolute)
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return fmt.Errorf("read %s: %w", absolute, err)
	}
	validation := policy.ValidateHTML(content)
	if len(validation.Errors) > 0 {
		return fmt.Errorf("HTML validation failed:\n  - %s", strings.Join(validation.Errors, "\n  - "))
	}
	filename := filepath.Base(absolute)
	if suppliedName, supplied := options["name"]; supplied {
		filename = suppliedName
	}
	filename, filenameErrors := policy.ValidateFilename(filename)
	if err := policy.FilenameError(filenameErrors); err != nil {
		return err
	}
	drafts := a.store.LoadDrafts()
	draftID := options["draft"]
	if _, newDraft := options["new"]; !newDraft && draftID == "" {
		draftID = drafts[absolute].DraftID
	}
	request := api.UploadRequest{HTML: string(content), Filename: filename, DraftID: draftID, HostingMode: "domain"}
	if password := options["password"]; password != "" {
		request.Access = &api.UploadAccess{Mode: "password", Password: password}
	}
	if raw := options["email"]; raw != "" {
		request.Access = &api.UploadAccess{Mode: "email", Emails: optionValues(raw)}
	}
	if makePublic {
		request.Access = &api.UploadAccess{Mode: "public"}
	}
	request.Env, err = parseAssignments(optionValues(options["env"]))
	if err != nil {
		return fmt.Errorf("--env: %w", err)
	}
	request.Secrets, err = parseAssignments(optionValues(options["secret"]))
	if err != nil {
		return fmt.Errorf("--secret: %w", err)
	}
	if description, supplied := options["description"]; supplied {
		request.Description = &description
	}
	if raw := options["temporary"]; raw != "" {
		seconds, err := parseDuration(raw)
		if err != nil {
			return err
		}
		request.ExpiresInSeconds = &seconds
	}
	if _, ok := options["workers-dev"]; ok {
		request.HostingMode = "workers_dev"
	}
	hash := sha256.Sum256(content)
	request.Metadata = provenance.Collect(ctx, absolute, a.version, hex.EncodeToString(hash[:]))
	response, err := client.Upload(ctx, request)
	if err != nil {
		return err
	}
	expiresAt := ""
	if response.ExpiresAt != nil {
		expiresAt = *response.ExpiresAt
	}
	drafts[absolute] = state.Draft{DraftID: response.DraftID, PublicURL: response.PublicURL, RawURL: response.RawURL, LatestVersionNumber: response.VersionNumber, HostingMode: response.HostingMode, ExpiresAt: expiresAt, UpdatedAt: a.now().UTC()}
	if err := a.store.SaveDrafts(drafts); err != nil {
		return fmt.Errorf("upload succeeded but local draft mapping could not be saved: %w", err)
	}
	action := "Uploaded"
	if response.VersionNumber > 1 || draftID != "" {
		action = "Updated"
	}
	fmt.Fprintf(a.out, "%s draft %s (version %d)\nPublic URL: %s\nRaw URL: %s\n", action, response.DraftID, response.VersionNumber, response.PublicURL, response.RawURL)
	if response.VersionURL != "" {
		fmt.Fprintf(a.out, "Version URL: %s\n", response.VersionURL)
	}
	if response.WorkersDevURL != nil {
		fmt.Fprintf(a.out, "workers.dev URL: %s\n", *response.WorkersDevURL)
	}
	if response.ExpiresAt != nil {
		if expiry, err := time.Parse(time.RFC3339Nano, *response.ExpiresAt); err == nil {
			fmt.Fprintf(a.out, "Expires: %s (%s remaining)\n", expiry.UTC().Format(time.RFC3339), friendlyDuration(expiry.Sub(a.now())))
		} else {
			fmt.Fprintf(a.out, "Expires: %s\n", *response.ExpiresAt)
		}
	}
	for _, warning := range uniqueStrings(append(validation.Warnings, response.Warnings...)) {
		fmt.Fprintf(a.errOut, "warning: %s\n", warning)
	}
	return nil
}

func (a *App) list(ctx context.Context, args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry list [--api-url <url>] [--json]")
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true, "json": false})
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return errors.New("usage: pageferry list [--api-url <url>] [--json]")
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	drafts, err := client.Drafts(ctx)
	if err != nil {
		return err
	}
	if _, jsonOutput := options["json"]; jsonOutput {
		encoded, err := json.MarshalIndent(drafts, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(a.out, string(encoded))
		return nil
	}
	if len(drafts) == 0 {
		fmt.Fprintln(a.out, "No drafts yet. Publish one with: pageferry upload <file>")
		return nil
	}
	for index, draft := range drafts {
		if index > 0 {
			fmt.Fprintln(a.out)
		}
		repository := "no repo"
		if draft.RepoOrg != nil && draft.RepoName != nil {
			repository = *draft.RepoOrg + "/" + *draft.RepoName
		}
		version := "-"
		if draft.LatestVersionNumber != nil {
			version = strconv.Itoa(*draft.LatestVersionNumber)
		}
		updated := relativeTime(draft.UpdatedAt, a.now())
		states := []string{draft.HostingMode}
		if draft.ExpiresAt == nil {
			states = append(states, "permanent")
		} else if draft.Expired {
			states = append(states, "expired "+*draft.ExpiresAt)
		} else {
			expiryState := "expires " + *draft.ExpiresAt
			if expiry, err := time.Parse(time.RFC3339Nano, *draft.ExpiresAt); err == nil {
				expiryState += " (" + friendlyDuration(expiry.Sub(a.now())) + " remaining)"
			}
			states = append(states, expiryState)
		}
		if draft.Disabled {
			states = append(states, "disabled")
		}
		fmt.Fprintf(a.out, "%s — %s\n  %s · v%s · %d versions · updated %s · %s\n  %s\n", draft.Title, repository, draft.DraftID, version, draft.VersionCount, updated, strings.Join(states, " · "), draft.PublicURL)
		if draft.Description != nil && *draft.Description != "" {
			fmt.Fprintf(a.out, "  %s\n", *draft.Description)
		}
	}
	return nil
}

func (a *App) skill(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry skill install --agent <opencode|codex|claude|cursor|all> [--local|--global] [--force]")
		return nil
	}
	if args[0] != "install" {
		return fmt.Errorf("unknown skill command %q", args[0])
	}
	if len(args) == 2 && isHelp(args[1]) {
		fmt.Fprintln(a.out, "Usage: pageferry skill install --agent <opencode|codex|claude|cursor|all> [--local|--global] [--force]")
		return nil
	}
	options, positional, err := parseOptions(args[1:], map[string]bool{"agent": true, "local": false, "global": false, "force": false})
	if err != nil {
		return err
	}
	if len(positional) != 0 || options["agent"] == "" {
		return errors.New("usage: pageferry skill install --agent <agent|all> [--local|--global] [--force]")
	}
	_, local := options["local"]
	_, global := options["global"]
	if local && global {
		return errors.New("--local and --global cannot be used together")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_, force := options["force"]
	paths, err := skill.Install(options["agent"], global, force, cwd, home)
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintf(a.out, "Installed PageFerry skill: %s\n", path)
	}
	return nil
}

func (a *App) authenticatedClient(explicitOrigin string) (*api.Client, error) {
	key := strings.TrimSpace(a.getenv("PAGEFERRY_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(a.store.LoadCredentials().APIKey)
	}
	if key == "" {
		return nil, errors.New("no API key configured; run `pageferry auth login` or set PAGEFERRY_API_KEY")
	}
	origin, err := a.origin(explicitOrigin)
	if err != nil {
		return nil, err
	}
	return a.client(origin, key), nil
}

func (a *App) client(origin, key string) *api.Client {
	return &api.Client{Origin: origin, APIKey: key, Version: a.version}
}

func (a *App) origin(explicit string) (string, error) {
	raw := explicit
	if raw == "" {
		raw = a.getenv("PAGEFERRY_API_URL")
	}
	if raw == "" {
		raw = a.store.LoadConfig().APIURL
	}
	if raw == "" {
		raw = defaultAPIURL
	}
	return api.ValidateOrigin(raw)
}

func parseDuration(raw string) (int64, error) {
	lower := strings.ToLower(raw)
	if strings.HasSuffix(lower, "d") {
		days, err := strconv.ParseInt(strings.TrimSuffix(lower, "d"), 10, 64)
		if err == nil && days >= 1 && days <= 30 {
			return days * 24 * 60 * 60, nil
		}
		return 0, fmt.Errorf("temporary duration must be an exact duration from 5m through 30d")
	}
	if strings.ContainsAny(lower, "yw") {
		return 0, fmt.Errorf("invalid temporary duration %q; use exact units such as 30m or 24h", raw)
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration%time.Second != 0 || duration < 5*time.Minute || duration > 30*24*time.Hour {
		return 0, fmt.Errorf("temporary duration must be an exact duration from 5m through 30d")
	}
	return int64(duration / time.Second), nil
}

func parseOptions(args []string, spec map[string]bool) (map[string]string, []string, error) {
	options := make(map[string]string)
	var positional []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(argument, "--") {
			positional = append(positional, argument)
			continue
		}
		nameValue := strings.SplitN(strings.TrimPrefix(argument, "--"), "=", 2)
		name := nameValue[0]
		requiresValue, ok := spec[name]
		if !ok {
			return nil, nil, fmt.Errorf("unknown option --%s", name)
		}
		if !requiresValue {
			if len(nameValue) == 2 {
				return nil, nil, fmt.Errorf("option --%s does not take a value", name)
			}
			options[name] = "true"
			continue
		}
		if len(nameValue) == 2 {
			options[name] = appendOption(options[name], nameValue[1])
			continue
		}
		index++
		if index >= len(args) || strings.HasPrefix(args[index], "--") {
			return nil, nil, fmt.Errorf("option --%s requires a value", name)
		}
		options[name] = appendOption(options[name], args[index])
	}
	return options, positional, nil
}

func appendOption(existing, value string) string {
	if existing == "" {
		return value
	}
	return existing + "\x00" + value
}

func optionValues(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, "\x00")
}

func parseAssignments(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(values))
	for _, assignment := range values {
		parts := strings.SplitN(assignment, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, fmt.Errorf("expected NAME=value")
		}
		name := parts[0]
		if name[0] < 'A' || name[0] > 'Z' {
			return nil, fmt.Errorf("invalid variable name %q", name)
		}
		for _, char := range name {
			if !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '_' {
				return nil, fmt.Errorf("invalid variable name %q", name)
			}
		}
		result[name] = parts[1]
	}
	return result, nil
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func relativeTime(raw string, now time.Time) string {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	delta := now.Sub(value)
	if delta < time.Minute {
		return "just now"
	}
	if delta < time.Hour {
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	}
	if delta < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
}

func friendlyDuration(duration time.Duration) string {
	if duration <= 0 {
		return "expired"
	}
	if duration >= 48*time.Hour {
		return fmt.Sprintf("%dd", int(duration.Hours()/24))
	}
	if duration >= time.Hour {
		return fmt.Sprintf("%dh%dm", int(duration.Hours()), int(duration.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(duration.Minutes()))
}

func isHelp(value string) bool { return value == "--help" || value == "-h" || value == "help" }

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
