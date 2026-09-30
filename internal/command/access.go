package command

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gallez-tech/pageferry-cli/internal/api"
)

const accessUsage = "pageferry access <draft-id|file> [--public | --password <password> | --email <address>] [--sign-out-readers] [--api-url <url>]"

var draftIDPattern = regexp.MustCompile(`^[a-z0-9]{12}$`)
var accessEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (a *App) access(ctx context.Context, args []string) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+accessUsage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{
		"api-url":          true,
		"public":           false,
		"password":         true,
		"email":            true,
		"sign-out-readers": false,
	})
	if err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return errors.New("usage: " + accessUsage)
	}
	request, hasAccessChange, err := accessRequest(options)
	if err != nil {
		return err
	}
	_, signOut := options["sign-out-readers"]
	if !hasAccessChange && !signOut {
		return errors.New("specify one of --public, --password, --email, or --sign-out-readers")
	}
	draftID, err := a.resolveDraftID(positional[0])
	if err != nil {
		return err
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	if hasAccessChange {
		result, err := client.UpdateDraftAccess(ctx, draftID, *request)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Access mode: %s\n", result.AccessMode)
		if result.AccessMode == "email" {
			fmt.Fprintf(a.out, "Reader emails: %s\n", strings.Join(result.AccessEmails, ", "))
		}
	}
	if signOut {
		if err := client.SignOutDraftReaders(ctx, draftID); err != nil {
			return err
		}
		fmt.Fprintln(a.out, "Readers signed out.")
	}
	return nil
}

func (a *App) resolveDraftID(target string) (string, error) {
	if draftIDPattern.MatchString(target) {
		return target, nil
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve draft path: %w", err)
	}
	draftID := a.store.LoadDrafts()[absolute].DraftID
	if draftID == "" {
		return "", fmt.Errorf("no saved draft for %s; upload it first or pass its 12-character draft ID", absolute)
	}
	return draftID, nil
}

// accessRequest validates and converts the shared upload/access flags. Its
// rules mirror the API's access resolver so invalid requests fail locally.
func accessRequest(options map[string]string) (*api.DraftAccessRequest, bool, error) {
	_, makePublic := options["public"]
	password, hasPassword := options["password"]
	rawEmails, hasEmails := options["email"]
	if boolCount(makePublic, hasPassword, hasEmails) > 1 {
		return nil, false, errors.New("--public, --password, and --email are mutually exclusive")
	}
	if !makePublic && !hasPassword && !hasEmails {
		return nil, false, nil
	}
	if hasPassword {
		if len(strings.TrimSpace(password)) < 8 {
			return nil, false, errors.New("page password must be at least 8 characters")
		}
		return &api.DraftAccessRequest{Mode: "password", Password: password}, true, nil
	}
	if hasEmails {
		emails := make([]string, 0, len(optionValues(rawEmails)))
		seen := make(map[string]bool)
		for _, email := range optionValues(rawEmails) {
			email = strings.ToLower(strings.TrimSpace(email))
			if email == "" || !accessEmailPattern.MatchString(email) {
				return nil, false, errors.New("invalid access email")
			}
			if !seen[email] {
				seen[email] = true
				emails = append(emails, email)
			}
		}
		if len(emails) == 0 {
			return nil, false, errors.New("email access requires at least one email")
		}
		return &api.DraftAccessRequest{Mode: "email", Emails: emails}, true, nil
	}
	return &api.DraftAccessRequest{Mode: "public"}, true, nil
}
