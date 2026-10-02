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
	"os/exec"
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
	build   func(ctx context.Context, dir string, output io.Writer) error
}

func New(version string, in io.Reader, out, errOut io.Writer) (*App, error) {
	store, err := state.Default()
	if err != nil {
		return nil, err
	}
	return &App{version: version, in: in, out: out, errOut: errOut, store: store, now: time.Now, getenv: os.Getenv, build: npmBuild}, nil
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
	case "keys":
		return a.keys(ctx, args[1:])
	case "access":
		return a.access(ctx, args[1:])
	case "skill":
		return a.skill(args[1:])
	case "update":
		return a.update(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q; use --help", args[0])
	}
}

func (a *App) printHelp() {
	fmt.Fprint(a.out, `PageFerry publishes a local HTML document or static site as a stable public URL.

Usage:
  pageferry auth set <api-key> [--name <key-name>] [--api-url <url>]
  pageferry auth login [--name <key-name>] [--api-url <url>]
  pageferry whoami [--api-url <url>]
  pageferry keys list [--json] [--api-url <url>]
  pageferry keys create [--name <key-name>] [--api-url <url>]
  pageferry keys rename <key-id> <key-name> [--api-url <url>]
  pageferry keys revoke <key-id> [--api-url <url>]
  pageferry access <draft-id|file> [--public | --password <password> | --email <address>]
                 [--sign-out-readers] [--api-url <url>]
  pageferry upload <file|directory> [--draft <id>] [--new] [--name <filename>]
                   [--build] [--description <text>] [--temporary <duration>]
                   [--public | --password <password> | --email <address>]
                   [--env <NAME=value>] [--secret <NAME=value>]
                   [--workers-dev] [--api-url <url>]
  pageferry validate <file|directory> [--name <filename>]
  pageferry list [--api-url <url>] [--json]
  pageferry skill install --agent <opencode|codex|claude|cursor|all>
                          [--local|--global] [--force]
                          (opencode/codex/cursor → .agents/skills; claude → .claude/skills)
  pageferry update check
  pageferry --version

Static sites:
  Pass a build output directory (with index.html at its root) to publish every
  file, e.g. a Slidev deck: npm run build && pageferry upload dist
  Given a project directory, PageFerry uses its dist/ folder; --build runs
  "npm run build" there first.

Environment:
  PAGEFERRY_API_URL  Override the saved API origin.
  PAGEFERRY_API_KEY  Override the saved API key.
`)
}

func (a *App) validate(args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: pageferry validate <file|directory> [--name <filename>]")
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"name": true})
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("usage: pageferry validate <file|directory> [--name <filename>]")
	}
	absolute, err := filepath.Abs(positional[0])
	if err != nil {
		return fmt.Errorf("resolve file: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("open %s: %w", absolute, err)
	}
	if info.IsDir() {
		if _, supplied := options["name"]; supplied {
			return errors.New("--name applies to single HTML files only")
		}
		site, err := a.collectSite(absolute)
		if err != nil {
			return err
		}
		for _, warning := range site.Warnings {
			fmt.Fprintf(a.errOut, "warning: %s\n", warning)
		}
		fmt.Fprintf(a.out, "Valid PageFerry site: %s (%d files, %s)\n", site.Root, len(site.Files), humanBytes(site.Bytes))
		return nil
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
			fmt.Fprintln(a.out, "Usage: pageferry auth set <api-key> [--name <key-name>] [--api-url <url>]")
			return nil
		}
		options, positional, err := parseOptions(args[1:], map[string]bool{"api-url": true, "name": true})
		if err != nil {
			return err
		}
		if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
			return errors.New("usage: pageferry auth set <api-key> [--name <key-name>] [--api-url <url>]")
		}
		key := strings.TrimSpace(positional[0])
		if name, ok := options["name"]; ok {
			origin, err := a.origin(options["api-url"])
			if err != nil {
				return err
			}
			if _, err := a.nameKey(ctx, a.client(origin, key), name); err != nil {
				return err
			}
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
		credentials := state.Credentials{APIKey: key, UpdatedAt: a.now().UTC()}
		if err := a.store.SaveCredentials(credentials); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
		fmt.Fprintln(a.out, "Credentials saved.")
		return nil
	case "login":
		if len(args) == 2 && isHelp(args[1]) {
			fmt.Fprintln(a.out, "Usage: pageferry auth login [--name <key-name>] [--api-url <url>]")
			return nil
		}
		options, positional, err := parseOptions(args[1:], map[string]bool{"api-url": true, "name": true})
		if err != nil {
			return err
		}
		if len(positional) != 0 {
			return errors.New("usage: pageferry auth login [--name <key-name>] [--api-url <url>]")
		}
		if name, ok := options["name"]; ok && strings.TrimSpace(name) == "" {
			return errors.New("--name must not be empty")
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
		client := a.client(origin, key)
		identity, err := client.Me(ctx)
		if err != nil {
			return fmt.Errorf("validate API key: %w", err)
		}
		if name, ok := options["name"]; ok {
			if identity, err = a.nameKey(ctx, client, name); err != nil {
				return err
			}
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
		fmt.Fprintln(a.out, "Usage: pageferry upload <file|directory> [--draft <id>] [--new] [--name <filename>] [--build] [--description <text>] [--temporary <duration>] [--public | --password <password> | --email <address>] [--env <NAME=value>] [--secret <NAME=value>] [--workers-dev] [--api-url <url>]")
		return nil
	}
	spec := map[string]bool{"api-url": true, "draft": true, "new": false, "name": true, "description": true, "temporary": true, "public": false, "password": true, "email": true, "env": true, "secret": true, "workers-dev": false, "build": false}
	options, positional, err := parseOptions(args, spec)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("usage: pageferry upload <file|directory> [options]")
	}
	if _, newDraft := options["new"]; newDraft && options["draft"] != "" {
		return errors.New("--new and --draft cannot be used together")
	}
	if _, workersDev := options["workers-dev"]; workersDev && options["temporary"] == "" {
		return errors.New("--workers-dev requires --temporary")
	}
	access, _, err := accessRequest(options)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(positional[0])
	if err != nil {
		return fmt.Errorf("resolve file: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("open %s: %w", absolute, err)
	}
	_, build := options["build"]
	if info.IsDir() {
		if _, supplied := options["name"]; supplied {
			return errors.New("--name applies to single HTML files only")
		}
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", absolute)
	} else if build {
		return errors.New("--build requires a project directory")
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	var (
		content  []byte
		filename string
		site     policy.SiteResult
		warnings []string
	)
	if info.IsDir() {
		if build {
			fmt.Fprintf(a.errOut, "Running npm run build in %s\n", absolute)
			if err := a.build(ctx, absolute, a.errOut); err != nil {
				return fmt.Errorf("npm run build: %w", err)
			}
		}
		site, err = a.collectSite(absolute)
		if err != nil {
			return err
		}
		warnings = site.Warnings
	} else {
		content, err = os.ReadFile(absolute)
		if err != nil {
			return fmt.Errorf("read %s: %w", absolute, err)
		}
		validation := policy.ValidateHTML(content)
		if len(validation.Errors) > 0 {
			return fmt.Errorf("HTML validation failed:\n  - %s", strings.Join(validation.Errors, "\n  - "))
		}
		warnings = validation.Warnings
		filename = filepath.Base(absolute)
		if suppliedName, supplied := options["name"]; supplied {
			filename = suppliedName
		}
		var filenameErrors []string
		filename, filenameErrors = policy.ValidateFilename(filename)
		if err := policy.FilenameError(filenameErrors); err != nil {
			return err
		}
	}
	drafts := a.store.LoadDrafts()
	draftID := options["draft"]
	if _, newDraft := options["new"]; !newDraft && draftID == "" {
		draftID = drafts[absolute].DraftID
	}
	request := api.UploadRequest{HTML: string(content), Filename: filename, DraftID: draftID, HostingMode: "domain"}
	if access != nil {
		request.Access = &api.UploadAccess{Mode: access.Mode, Password: access.Password, Emails: access.Emails}
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
	var response api.UploadResponse
	if info.IsDir() {
		files := make([]api.SiteFile, len(site.Files))
		for index, file := range site.Files {
			files[index] = api.SiteFile{Path: file.Path, Absolute: file.Absolute}
		}
		hash, digestErr := siteDigest(site.Files)
		if digestErr != nil {
			return digestErr
		}
		request.Metadata = provenance.Collect(ctx, site.Root, a.version, hash)
		fmt.Fprintf(a.errOut, "Uploading %d files (%s) from %s\n", len(files), humanBytes(site.Bytes), site.Root)
		response, err = client.UploadSite(ctx, request, files)
	} else {
		hash := sha256.Sum256(content)
		request.Metadata = provenance.Collect(ctx, absolute, a.version, hex.EncodeToString(hash[:]))
		response, err = client.Upload(ctx, request)
	}
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
	fmt.Fprintf(a.out, "%s draft %s (version %d)\nPublic URL: %s\n", action, response.DraftID, response.VersionNumber, response.PublicURL)
	if response.ContentKind == "site" {
		fmt.Fprintf(a.out, "Files: %d\n", response.FileCount)
	} else {
		fmt.Fprintf(a.out, "Raw URL: %s\n", response.RawURL)
	}
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
	for _, warning := range uniqueStrings(append(warnings, response.Warnings...)) {
		fmt.Fprintf(a.errOut, "warning: %s\n", warning)
	}
	return nil
}

// collectSite resolves the directory to publish: the directory itself when it
// holds index.html, otherwise its dist/ build output (Slidev, Vite, …).
func (a *App) collectSite(directory string) (policy.SiteResult, error) {
	root := directory
	if _, err := os.Stat(filepath.Join(root, policy.SiteIndex)); err != nil {
		dist := filepath.Join(directory, "dist")
		if _, distErr := os.Stat(filepath.Join(dist, policy.SiteIndex)); distErr != nil {
			return policy.SiteResult{}, fmt.Errorf("no index.html in %s or %s; build the site first (e.g. npm run build) or pass --build", directory, dist)
		}
		root = dist
	}
	site, err := policy.CollectSite(root)
	if err != nil {
		return site, err
	}
	if root != directory {
		site.Warnings = append(site.Warnings, policy.SlidevSourceWarnings(directory)...)
	} else if filepath.Base(root) == "dist" {
		site.Warnings = append(site.Warnings, policy.SlidevSourceWarnings(filepath.Dir(root))...)
	}
	site.Warnings = uniqueStrings(site.Warnings)
	for _, skipped := range site.Skipped {
		fmt.Fprintf(a.errOut, "skipped: %s\n", skipped)
	}
	if len(site.Errors) > 0 {
		return site, fmt.Errorf("site validation failed:\n  - %s", strings.Join(site.Errors, "\n  - "))
	}
	return site, nil
}

// siteDigest fingerprints a bundle as the hash of its "path NUL sha256" lines.
func siteDigest(files []policy.SiteFile) (string, error) {
	digest := sha256.New()
	for _, file := range files {
		source, err := os.Open(file.Absolute)
		if err != nil {
			return "", err
		}
		fileDigest := sha256.New()
		_, err = io.Copy(fileDigest, source)
		source.Close()
		if err != nil {
			return "", fmt.Errorf("read %s: %w", file.Absolute, err)
		}
		fmt.Fprintf(digest, "%s\x00%x\n", file.Path, fileDigest.Sum(nil))
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func npmBuild(ctx context.Context, dir string, output io.Writer) error {
	command := exec.CommandContext(ctx, "npm", "run", "build")
	command.Dir = dir
	command.Stdout = output
	command.Stderr = output
	return command.Run()
}

func humanBytes(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
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
		if draft.ContentKind == "site" {
			states = append(states, "site")
		}
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
	results, err := skill.Install(options["agent"], global, force, cwd, home)
	if err != nil {
		return err
	}
	for _, result := range results {
		switch result.Status {
		case skill.Current:
			fmt.Fprintf(a.out, "PageFerry skill is up to date: %s\n", result.Path)
		case skill.Updated:
			fmt.Fprintf(a.out, "Updated PageFerry skill: %s\n", result.Path)
		default:
			fmt.Fprintf(a.out, "Installed PageFerry skill: %s\n", result.Path)
		}
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
