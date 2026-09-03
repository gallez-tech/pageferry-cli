package provenance

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Collect(ctx context.Context, filePath, version, hash string) map[string]any {
	metadata := map[string]any{
		"cliVersion":       version,
		"contentSha256":    hash,
		"originalFilename": filepath.Base(filePath),
	}
	directory := filepath.Dir(filePath)
	root := git(ctx, directory, "rev-parse", "--show-toplevel")
	if root != "" {
		metadata["gitBranch"] = git(ctx, root, "branch", "--show-current")
		metadata["gitCommitSha"] = git(ctx, root, "rev-parse", "HEAD")
		metadata["gitCommitSubject"] = git(ctx, root, "log", "-1", "--pretty=%s")
		if output, err := runGit(ctx, root, "status", "--porcelain"); err == nil {
			metadata["gitDirty"] = strings.TrimSpace(output) != ""
		}
		host, org, repo := parseRemote(git(ctx, root, "config", "--get", "remote.origin.url"))
		if repo == "" {
			repo = filepath.Base(root)
			org = filepath.Base(filepath.Dir(root))
		}
		put(metadata, "repoHost", host)
		put(metadata, "repoOrg", org)
		put(metadata, "repoName", repo)
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		metadata["ciProvider"] = "github-actions"
		put(metadata, "ciActor", os.Getenv("GITHUB_ACTOR"))
		server, repository, runID := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID")
		if server != "" && repository != "" && runID != "" {
			metadata["ciRunUrl"] = strings.TrimRight(server, "/") + "/" + repository + "/actions/runs/" + runID
		}
	} else if os.Getenv("CI") != "" {
		metadata["ciProvider"] = "generic"
	}
	return metadata
}

func git(ctx context.Context, directory string, args ...string) string {
	output, err := runGit(ctx, directory, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

func runGit(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	command.Stderr = nil
	output, err := command.Output()
	return string(output), err
}

func parseRemote(remote string) (host, org, repo string) {
	remote = strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if remote == "" {
		return "", "", ""
	}
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil {
			return "", "", ""
		}
		host = parsed.Hostname()
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 2 {
			org, repo = parts[len(parts)-2], parts[len(parts)-1]
		}
		return
	}
	if at := strings.LastIndex(remote, "@"); at >= 0 {
		remote = remote[at+1:]
	}
	parts := strings.SplitN(remote, ":", 2)
	if len(parts) != 2 {
		return "", "", ""
	}
	host = parts[0]
	pathParts := strings.Split(strings.Trim(parts[1], "/"), "/")
	if len(pathParts) >= 2 {
		org, repo = pathParts[len(pathParts)-2], pathParts[len(pathParts)-1]
	}
	return
}

func put(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}
