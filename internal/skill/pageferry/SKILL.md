---
name: pageferry
description: Publish safe static HTML documents with PageFerry and retrieve PageFerry draft URLs. Use when sharing a plan, proposal, report, brief, or other standalone HTML artifact through PageFerry, or when the user supplies a PageFerry URL.
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
