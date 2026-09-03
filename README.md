# PageFerry CLI

The native Go command-line client for PageFerry.

```sh
go install github.com/gallez-tech/pageferry/cli/cmd/pageferry@latest
pageferry auth login
pageferry upload report.html
pageferry list
```

The API key can also be configured non-interactively:

```sh
pageferry auth set pf_your_key
PAGEFERRY_API_KEY=pf_your_key pageferry whoami
```

Use `PAGEFERRY_API_URL` or a command's `--api-url` option to target a custom server.

## Agent skill

Install the bundled PageFerry skill for one supported coding agent. Project-local installation
is the default; it resolves to the current Git worktree root.

```sh
pageferry skill install --agent codex
pageferry skill install --agent cursor --global
pageferry skill install --agent all
```

Supported agents are OpenCode, Codex, Claude Code, and Cursor Agent. Existing skill files are
preserved unless `--force` is supplied.

## Release artifacts

Every push to `main` or `master` builds downloadable workflow artifacts for Linux, macOS, and
Windows on amd64 and arm64. Windows artifacts include native MSI installers that install
`pageferry.exe` under Program Files and add PageFerry to the system `PATH`.

Tags matching `v*` publish the same artifacts as a GitHub Release with checksums. Release
artifacts are currently unsigned.

## Development

```sh
go test ./...
go vet ./...
```

Application packages belong under `internal/`; the executable entry point stays in
`cmd/pageferry/`.
