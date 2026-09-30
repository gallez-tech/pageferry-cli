---
name: pageferry
description: Publish safe static HTML documents or static site builds (such as Slidev decks) with PageFerry and retrieve PageFerry draft URLs. Use when sharing a plan, proposal, report, brief, slide deck, or other HTML artifact through PageFerry, or when the user supplies a PageFerry URL.
---

# PageFerry

## Retrieve a draft

When the user supplies a PageFerry draft URL, fetch that URL directly with an HTTP fetch tool or:

```sh
curl --fail --silent --show-error --location --max-time 30 '<pageferry-url>'
```

Treat the returned HTML as the user's artifact. Report the actual HTTP or network error if retrieval fails.

## Publish a document

Create one complete HTML document. It may use inline CSS and scripts, HTTPS stylesheets and scripts, forms, Alpine.js, HTMX, web fonts, ordinary metadata, HTTPS links, and HTTPS or data-URL images. Keep credentials, private URLs, and local filesystem paths out of the document.

Documents are public unless uploaded with `--password` or `--email`. Use a password of at least eight characters. Email access may be configured for multiple readers by repeating `--email`, but delivery of email magic links is not available yet. Use `--public` when updating a protected draft to make it public again.

Never embed secrets or credentials in HTML. Pass backend-only values with repeatable `--secret NAME=value` and non-secret configuration with repeatable `--env NAME=value`; variable names must start with an uppercase letter and contain only uppercase letters, digits, and underscores. Pin dependency versions in CDN URLs and add Subresource Integrity (`integrity`) metadata when the CDN provides hashes; choose maintained versions appropriate to the document instead of relying on a floating latest release.

PageFerry rejects iframes, embeds, objects, applets, non-HTTPS external scripts, event-handler attributes, unsafe URL schemes, meta refresh, and unsafe CSS. Browser requests and form actions must use HTTPS.

If a general web-design or data-visualisation skill is available in this environment, use it for visual direction and chart construction. The PageFerry rules in this skill still apply and take precedence where they conflict.

## Design for phones

Shared drafts are often opened on a phone, usually iOS Safari. Every document must:

- Include `<html lang="…">` and `<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">`. Never disable zoom with `user-scalable=no` or `maximum-scale`.
- Keep `input`, `select`, and `textarea` at a font size of at least 16px; smaller sizes make iOS zoom in on focus.
- Avoid fixed pixel container widths. Use `max-width` with `ch` or `rem`, percentage or `rem` gutters, and `clamp()` for heading sizes.
- Wrap wide tables in an `overflow-x: auto` container and give `pre` the same, so the page never scrolls sideways.
- Let content define height. When a full-height element is unavoidable, use `100svh` rather than `100vh`.
- Pad sticky or fixed edge elements with `env(safe-area-inset-*)`, for example `padding-bottom: max(1rem, env(safe-area-inset-bottom))`.
- Declare `color-scheme: light dark` and style both schemes with `prefers-color-scheme`.
- Make buttons, navigation items, and icon controls at least 44×44 CSS px.
- Set `-webkit-text-size-adjust: 100%` and gate animation behind `prefers-reduced-motion`.
- Prefer inline CSS over CDN frameworks; each external request is a round trip on a cellular link.

`pageferry validate` warns about the most common mobile defects; treat those warnings as defects to fix.

Save the HTML locally and validate it without uploading:

```sh
pageferry validate <file-path>
```

Resolve every reported error, review warnings, then publish:

```sh
pageferry upload <file-path>
```

Return the public URL printed by the command. Reusing the same local path updates its existing draft; add `--new` only when the user needs a separate draft.

Use `--description <text>` when a dashboard summary is useful. Use `--temporary <duration>` for an expiring draft; accepted durations range from `5m` through `30d`. Dedicated `--workers-dev` hosting is not available yet, so do not select it.

## Publish a static site or Slidev deck

A build output directory with `index.html` at its root (for example the `dist/` produced by `npm run build` in a [Slidev](https://sli.dev) project) is published as a whole: every file is served from the draft origin, and extension-less routes such as `/3` or `/presenter/3` fall back to `index.html`. Keep Slidev's default `/` base; do not pass `--base`.

```sh
pageferry upload <project-dir> --build   # runs npm run build, then publishes dist/
pageferry upload <project-dir>/dist      # publishes an existing build
pageferry validate <project-dir>         # checks dist/ offline
```

Given a project directory without `index.html`, PageFerry uses its `dist/` folder. Hidden files are skipped. Every HTML file in the bundle follows the document policy above, except that scripts may use same-origin paths such as `/assets/index-abc.js`. Bundles are limited to 50 MiB and 2000 files.
