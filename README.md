# PageFerry CLI

The native Go command-line client for PageFerry.

```sh
curl -fsSL https://raw.githubusercontent.com/gallez-tech/pageferry-cli/main/install.sh | sh
```

On Windows PowerShell, install the checksum-verified MSI with:

```powershell
irm https://raw.githubusercontent.com/gallez-tech/pageferry-cli/main/install.ps1 | iex
```

Or install from source:

```sh
go install github.com/gallez-tech/pageferry-cli/cmd/pageferry@latest
pageferry auth login
pageferry validate report.html
pageferry upload report.html
pageferry list
```

The API key can also be configured non-interactively:

```sh
pageferry auth set pf_your_key
PAGEFERRY_API_KEY=pf_your_key pageferry whoami
```

Use `PAGEFERRY_API_URL` or a command's `--api-url` option to target a custom server.

Check whether the installed release is current with:

```sh
pageferry update check
```

The install script supports Linux and macOS on amd64 and arm64, verifies the release
checksum, and installs to `~/.local/bin` by default. Override the destination with
`PAGEFERRY_INSTALL_DIR` or install a specific release with `PAGEFERRY_VERSION=v1.2.3`.
The PowerShell installer supports Windows amd64 and arm64, verifies the MSI checksum,
and requests elevation because the MSI installs PageFerry for the whole machine. It also
honors `PAGEFERRY_VERSION`.

## Interactive documents

PageFerry accepts complete HTML documents with inline CSS and scripts, HTTPS scripts and
stylesheets, ES modules, forms, Alpine.js, HTMX, web fonts, and HTTPS browser requests.
The CLI validates documents before upload and the server repeats validation at the trust
boundary. Unsafe URL schemes, non-HTTPS external scripts, iframes, embeds, objects,
applets, inline event-handler attributes, meta refresh, and unsafe CSS are rejected.

Validate a document locally without an API key or network access:

```sh
pageferry validate report.html
pageferry validate generated-output.tmp --name report.html
```

Publishing requires a valid key for the server's configured owner (or its operator
bootstrap key). Drafts are public by default. Protect one and store backend-only values:

```sh
pageferry upload app.html --password 'a long password' \
  --env WEBHOOK_URL=https://example.com/hook \
  --secret API_KEY=pf_private
pageferry upload app.html --email reader@example.com
```

These options are repeatable. Password and email access are mutually exclusive. Email
access is accepted, but magic-link delivery awaits the future SMTP/API integration. Use
`--public` on a later upload to remove access protection.

## Agent skill

Install the bundled PageFerry skill for one supported coding agent. Project-local installation
is the default; it resolves to the current Git worktree root.

```sh
pageferry skill install --agent codex
pageferry skill install --agent cursor --global
pageferry skill install --agent all
```

Re-run the applicable command with `--force` to refresh an existing installed copy after
updating PageFerry.

OpenCode, Codex, and Cursor share the portable Agent Skills path (`.agents/skills/` locally,
`~/.agents/skills/` globally). Claude Code still uses `.claude/skills/`. `--agent all` writes
both locations once. Existing skill files are preserved unless `--force` is supplied.

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

## Origins and acknowledgements

PageFerry grew from the idea demonstrated by
[Theo Browne (`@t3dotgg`)](https://github.com/t3dotgg) through the
[Postplan CLI](https://www.npmjs.com/package/postplan). PageFerry develops that idea as an
independent project with its own Go CLI. It is not affiliated with or endorsed by Postplan
or Theo Browne. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for licensing details.

## License

PageFerry is released under the [MIT License](LICENSE).
