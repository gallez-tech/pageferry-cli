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

PageFerry rejects iframes, embeds, objects, applets, non-HTTPS external scripts, event-handler attributes, unsafe URL schemes, meta refresh, and unsafe CSS. Browser requests and form actions must use HTTPS.

Save the HTML locally, then run:

```sh
pageferry upload <file-path>
```

Return the public URL printed by the command. Reusing the same local path updates its existing draft; add `--new` only when the user needs a separate draft.
