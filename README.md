# PageFerry CLI

The native Go command-line client for PageFerry.

```sh
curl -fsSL https://raw.githubusercontent.com/gallez-tech/pageferry-cli/main/install.sh | sh
```

On Windows PowerShell, install the checksum-verified MSI with:

```powershell
irm https://raw.githubusercontent.com/gallez-tech/pageferry-cli/main/install.ps1 | iex
```

Re-running either installer replaces the installed executable, including when reinstalling
the same PageFerry version.

Or install from source:

```sh
go install github.com/gallez-tech/pageferry-cli/cmd/pageferry@latest
pageferry auth login
pageferry validate report.html
pageferry upload report.html
pageferry list
```

`pageferry auth login` opens your configured PageFerry host in the browser. Sign in
and select **Authorize CLI**; the CLI creates and saves an API key named after your
computer's hostname. Use `--name` to choose another name. Login times out after five
minutes. If the browser runs on another device, authorize there: the final
`http://127.0.0.1:…/callback?…` page fails to load on that device, so copy its address and
paste it into the terminal to finish.

Over SSH (`SSH_CONNECTION`, `SSH_CLIENT`, or `SSH_TTY` set), `pageferry auth login` uses a
one-time login code instead; pass `--browser` to open a browser on the remote host anyway.
To choose the login-code flow explicitly, for example in a container, run:

```sh
pageferry auth login --headless
```

Open the printed URL on your phone or laptop, sign in, and select **Authorize CLI**.
Paste the `pf_login_…` code back into the VPS terminal. That code expires after two
minutes and can be used once, only by the CLI that started login. The CLI creates
and saves the API key without printing it. No browser or inbound port is required
on the VPS. Both login modes use the computer's hostname unless `--name` is set.

For agents, use two commands so the agent can hand off the link and resume later:

```sh
pageferry auth start --name "VPS agent"
# Open the printed link, authorize, and give the one-time code to the agent.
pageferry auth complete <login-code>
pageferry whoami
pageferry upload report.html
```

`auth complete --stdin` accepts the code from standard input. A pending request
expires after five minutes; starting another replaces it. The CLI stores the PKCE
verifier privately on the initiating machine and removes it after successful
login. Completion uses the host saved by `auth start`, even if environment settings
change between commands.

Agents use saved CLI credentials for subsequent commands. Login and publishing
keep the API key and PKCE verifier out of output; neither needs to be included in prompts,
agent configuration, or shell arguments. On Unix, the credential and pending-login
files use mode `0600` inside the `0700` PageFerry directory. This prevents accidental
sharing with the model; an agent with unrestricted access to the same OS account
can still read those files. Enforcing isolation requires a separate credential
broker or sandbox that restricts access to the credential store and broker.

`pageferry auth login --manual` keeps the original API-key copy-and-paste flow for
servers that do not yet support browser login.

The API key can also be configured non-interactively:

```sh
pageferry auth set pf_your_key
PAGEFERRY_API_KEY=pf_your_key pageferry whoami
```

Use `PAGEFERRY_API_URL` or a command's `--api-url` option to target a custom server.

Name keys so the dashboard shows which machine uses which, and manage them from the CLI:

```sh
pageferry auth login --name "Work laptop"
pageferry keys list                       # * marks the key in use
pageferry keys create --name "CI"         # prints the new key once
pageferry keys rename <key-id> "Nightly CI"
pageferry keys revoke <key-id>
```

Change a draft's access without publishing a new version. Pass the original upload
path (when it is saved locally) or its 12-character draft ID; `--sign-out-readers`
also invalidates every reader session and pending magic link:

```sh
pageferry access report.html --email reader@example.com
pageferry access abc123def456 --public --sign-out-readers
```

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

Validation also warns, without blocking upload, about markup that breaks on phones: a
missing or zoom-blocking viewport, a missing `<html lang>`, fixed pixel widths, form
controls below 16px (iOS Safari zooms on focus), and `100vh` without an `svh`/`dvh`
alternative.

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
`pageferry access <draft-id|file> --public` to remove access protection without re-publishing.

## Static sites and Slidev decks

`pageferry upload` also accepts a build output directory with `index.html` at its root. Every
file is published and served from the draft's own origin; extension-less routes (`/3`,
`/presenter/3`, `/overview`) fall back to `index.html`, so single-page apps built with
history routing work unchanged. This covers [Slidev](https://sli.dev/guide/) decks built with
`npm run build` (keep the default `/` base):

```sh
cd my-talk
npm run build && pageferry upload dist
# or let PageFerry run the build and pick up dist/ itself:
pageferry upload . --build
pageferry validate .
```

Hidden files and symbolic links are skipped (including dotfiles and symlinks; `404.html`,
`_redirects`, and hashed assets under `assets/` are kept). HTML files in the bundle follow
the same policy as single documents, except that scripts may load same-origin paths such as
`/assets/index-abc.js`. Bundles are limited to 50 MiB and 2000 files.

For [Slidev](https://sli.dev) builds, `pageferry validate` and `pageferry upload` detect
`slidev build` output (`meta property="slidev:version"`), check that every root-absolute script
and stylesheet path exists in the bundle (Monaco workers, PDF export, and so on), and emit
warnings only when something is actionable: rebuild without `slidev build --base`, include the
exported PDF when `download: true` is set in `slides.md` (`slidev build --download` with
`playwright-chromium`), trim a bundle near the 50 MiB limit, note that `drawings.persist: true`
is not synchronized between devices, or remind you that Slidev's `remote` password is not
enforced by PageFerry (use `--password` or `--email` instead). On PageFerry, published Slidev
decks synchronize presenter navigation across devices over HTTPS; YouTube, Tweet, and iframe
layouts work on the public URL. Screen mirror and recording still require a secure context
(HTTPS) and a browser permission prompt. Access options,
`--temporary`, `--description`, and draft updates work as for documents. Version URLs
(`/v/<n>/`) serve that version's files, but assets referenced with absolute paths load from
the current version.

## Agent skill

Install the bundled PageFerry skill for one supported coding agent. Project-local installation
is the default; it resolves to the current Git worktree root.

```sh
pageferry skill install --agent codex
pageferry skill install --agent cursor --global
pageferry skill install --agent all
```

Re-running the command reports whether the installed skill is already current. If it differs
from the skill bundled with the CLI, the command reports it as outdated; add `--force` to
replace it with the bundled version.

OpenCode, Codex, and Cursor share the portable Agent Skills path (`.agents/skills/` locally,
`~/.agents/skills/` globally). Claude Code still uses `.claude/skills/`. `--agent all` writes
both locations once. Differing skill files are preserved unless `--force` is supplied.

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
