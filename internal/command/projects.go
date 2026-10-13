package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gallez-tech/pageferry-cli/internal/api"
	"github.com/gallez-tech/pageferry-cli/internal/provenance"
)

const projectUsage = "Usage: pageferry project <list|create|edit|rename|delete|move> [arguments]"

func (a *App) project(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(a.out, projectUsage)
		fmt.Fprint(a.out, `
  pageferry project list [--json] [--api-url <url>]
  pageferry project create <name> [--slug <slug>] [--repo <org/name|url> | --from-git]
                           [--description <text>] [--api-url <url>]
  pageferry project edit <slug> [--name <name>] [--slug <new-slug>]
                         [--repo <org/name|url> | --from-git | --no-repo]
                         [--description <text>] [--api-url <url>]
  pageferry project rename <slug> <name> [--api-url <url>]
  pageferry project delete <slug> [--api-url <url>]
  pageferry project move <draft-id|file> <slug> [--api-url <url>]
  pageferry project move <draft-id|file> --none [--api-url <url>]

A project groups related pages and assets, with or without a git repository.
`)
		return nil
	}
	switch args[0] {
	case "list":
		return a.projectList(ctx, args[1:])
	case "create":
		return a.projectCreate(ctx, args[1:])
	case "edit":
		return a.projectEdit(ctx, args[1:])
	case "rename":
		return a.projectRename(ctx, args[1:])
	case "delete":
		return a.projectDelete(ctx, args[1:])
	case "move":
		return a.projectMove(ctx, args[1:])
	default:
		return fmt.Errorf("unknown project command %q", args[0])
	}
}

func (a *App) projectList(ctx context.Context, args []string) error {
	const usage = "pageferry project list [--json] [--api-url <url>]"
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
	projects, err := client.Projects(ctx)
	if err != nil {
		return err
	}
	if _, jsonOutput := options["json"]; jsonOutput {
		encoded, err := json.MarshalIndent(projects, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(a.out, string(encoded))
		return nil
	}
	if len(projects) == 0 {
		fmt.Fprintln(a.out, "No projects yet. Create one with: pageferry project create <name>")
		return nil
	}
	for index, project := range projects {
		if index > 0 {
			fmt.Fprintln(a.out)
		}
		repository := "no repo"
		if project.RepoURL != nil {
			repository = *project.RepoURL
		}
		fmt.Fprintf(a.out, "%s — %s\n  %s · %s · %s · updated %s\n", project.Name, project.Slug, plural(project.PageCount, "page"), plural(project.AssetCount, "asset"), repository, relativeTime(project.UpdatedAt, a.now()))
		if project.Description != nil && *project.Description != "" {
			fmt.Fprintf(a.out, "  %s\n", *project.Description)
		}
	}
	return nil
}

func (a *App) projectCreate(ctx context.Context, args []string) error {
	const usage = "pageferry project create <name> [--slug <slug>] [--repo <org/name|url> | --from-git] [--description <text>] [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true, "slug": true, "repo": true, "from-git": false, "description": true})
	if err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return errors.New("usage: " + usage)
	}
	request := api.ProjectRequest{Name: &positional[0]}
	if slug, ok := options["slug"]; ok {
		request.Slug = &slug
	}
	if description, ok := options["description"]; ok {
		request.Description = &description
	}
	if repo, ok, err := a.repoOption(ctx, options); err != nil {
		return err
	} else if ok {
		request.Repo = &repo
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	project, err := client.CreateProject(ctx, request)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Created project %s (%s)\nUse it with: pageferry upload <file> --project %s\n", project.Name, project.Slug, project.Slug)
	return nil
}

func (a *App) projectEdit(ctx context.Context, args []string) error {
	const usage = "pageferry project edit <slug> [--name <name>] [--slug <new-slug>] [--repo <org/name|url> | --from-git | --no-repo] [--description <text>] [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true, "name": true, "slug": true, "repo": true, "from-git": false, "no-repo": false, "description": true})
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("usage: " + usage)
	}
	var request api.ProjectRequest
	if name, ok := options["name"]; ok {
		request.Name = &name
	}
	if slug, ok := options["slug"]; ok {
		request.Slug = &slug
	}
	if description, ok := options["description"]; ok {
		request.Description = &description
	}
	if _, clear := options["no-repo"]; clear {
		if _, set := options["repo"]; set {
			return errors.New("--no-repo cannot be combined with --repo")
		}
		if _, set := options["from-git"]; set {
			return errors.New("--no-repo cannot be combined with --from-git")
		}
		empty := ""
		request.Repo = &empty
	} else if repo, ok, err := a.repoOption(ctx, options); err != nil {
		return err
	} else if ok {
		request.Repo = &repo
	}
	if request == (api.ProjectRequest{}) {
		return errors.New("nothing to change; pass --name, --slug, --repo, --from-git, --no-repo, or --description")
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	project, err := client.UpdateProject(ctx, positional[0], request)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Updated project %s (%s)\n", project.Name, project.Slug)
	return nil
}

func (a *App) projectRename(ctx context.Context, args []string) error {
	const usage = "pageferry project rename <slug> <name> [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true})
	if err != nil {
		return err
	}
	if len(positional) != 2 || strings.TrimSpace(positional[1]) == "" {
		return errors.New("usage: " + usage)
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	project, err := client.UpdateProject(ctx, positional[0], api.ProjectRequest{Name: &positional[1]})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Renamed project %s to %s\n", project.Slug, project.Name)
	return nil
}

func (a *App) projectDelete(ctx context.Context, args []string) error {
	const usage = "pageferry project delete <slug> [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true})
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("usage: " + usage)
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	if err := client.DeleteProject(ctx, positional[0]); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Deleted project %s. Its pages and assets were kept and are now unassigned.\n", positional[0])
	return nil
}

func (a *App) projectMove(ctx context.Context, args []string) error {
	const usage = "pageferry project move <draft-id|file> (<slug> | --none) [--api-url <url>]"
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprintln(a.out, "Usage: "+usage)
		return nil
	}
	options, positional, err := parseOptions(args, map[string]bool{"api-url": true, "none": false})
	if err != nil {
		return err
	}
	_, none := options["none"]
	if (none && len(positional) != 1) || (!none && len(positional) != 2) {
		return errors.New("usage: " + usage)
	}
	draftID, err := a.resolveDraftID(positional[0])
	if err != nil {
		return err
	}
	slug := ""
	if !none {
		slug = positional[1]
	}
	client, err := a.authenticatedClient(options["api-url"])
	if err != nil {
		return err
	}
	if err := client.MoveDraft(ctx, draftID, slug); err != nil {
		return err
	}
	if none {
		fmt.Fprintf(a.out, "Removed %s from its project.\n", draftID)
	} else {
		fmt.Fprintf(a.out, "Moved %s to project %s.\n", draftID, slug)
	}
	return nil
}

// repoOption reads --repo or --from-git. The second result reports whether
// either was given.
func (a *App) repoOption(ctx context.Context, options map[string]string) (string, bool, error) {
	repo, hasRepo := options["repo"]
	_, fromGit := options["from-git"]
	if hasRepo && fromGit {
		return "", false, errors.New("--repo and --from-git cannot be used together")
	}
	if hasRepo {
		return repo, true, nil
	}
	if !fromGit {
		return "", false, nil
	}
	directory, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("resolve current directory: %w", err)
	}
	host, org, name := provenance.Repository(ctx, directory)
	if org == "" || name == "" {
		return "", false, errors.New("--from-git needs a git repository with an origin remote in the current directory")
	}
	return host + "/" + org + "/" + name, true, nil
}

func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
