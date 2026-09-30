package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gallez-tech/pageferry-cli/internal/api"
)

const keysUsage = "Usage: pageferry keys <list|create|rename|revoke> [arguments]"

func (a *App) keys(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(a.out, keysUsage)
		return nil
	}
	switch args[0] {
	case "list":
		return a.keysList(ctx, args[1:])
	case "create":
		return a.keysCreate(ctx, args[1:])
	case "rename":
		return a.keysRename(ctx, args[1:])
	case "revoke":
		return a.keysRevoke(ctx, args[1:])
	default:
		return fmt.Errorf("unknown keys command %q", args[0])
	}
}

func (a *App) keysList(ctx context.Context, args []string) error {
	const usage = "pageferry keys list [--json] [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true, "json": false})
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return errors.New("usage: " + usage)
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	keys, err := client.APIKeys(ctx)
	if err != nil {
		return err
	}
	if _, jsonOutput := options["json"]; jsonOutput {
		encoded, err := json.MarshalIndent(keys, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(a.out, string(encoded))
		return nil
	}
	for _, key := range keys {
		marker := "  "
		if key.Current {
			marker = "* "
		}
		lastUsed := "never used"
		if key.LastUsedAt != nil {
			lastUsed = "used " + relativeTime(*key.LastUsedAt, a.now())
		}
		fmt.Fprintf(a.out, "%s%s — %s\n    created %s · %s\n", marker, key.Name, key.ID, relativeTime(key.CreatedAt, a.now()), lastUsed)
	}
	return nil
}

func (a *App) keysCreate(ctx context.Context, args []string) error {
	const usage = "pageferry keys create [--name <key-name>] [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true, "name": true})
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return errors.New("usage: " + usage)
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	created, err := client.CreateAPIKey(ctx, strings.TrimSpace(options["name"]))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Created API key %s (%s).\nCopy it now; it will not be shown again:\n%s\n", created.APIKey.Name, created.APIKey.ID, created.Token)
	return nil
}

func (a *App) keysRename(ctx context.Context, args []string) error {
	const usage = "pageferry keys rename <key-id> <key-name> [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true})
	if err != nil {
		return err
	}
	if len(positional) < 2 || strings.TrimSpace(positional[0]) == "" {
		return errors.New("usage: " + usage)
	}
	name := strings.TrimSpace(strings.Join(positional[1:], " "))
	if name == "" {
		return errors.New("usage: " + usage)
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	if err := client.RenameAPIKey(ctx, positional[0], name); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Renamed API key %s to %s.\n", positional[0], name)
	return nil
}

func (a *App) keysRevoke(ctx context.Context, args []string) error {
	const usage = "pageferry keys revoke <key-id> [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true})
	if err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return errors.New("usage: " + usage)
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	if err := client.RevokeAPIKey(ctx, positional[0]); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Revoked API key %s.\n", positional[0])
	return nil
}

// nameKey renames the key the client authenticates with and returns the
// refreshed identity.
func (a *App) nameKey(ctx context.Context, client *api.Client, name string) (api.Identity, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return api.Identity{}, errors.New("--name must not be empty")
	}
	identity, err := client.Me(ctx)
	if err != nil {
		return api.Identity{}, fmt.Errorf("validate API key: %w", err)
	}
	if err := client.RenameAPIKey(ctx, identity.APIKeyID, name); err != nil {
		return api.Identity{}, fmt.Errorf("name API key: %w", err)
	}
	identity.APIKeyName = name
	return identity, nil
}
