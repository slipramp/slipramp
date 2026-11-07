# Slipramp user guide

The public user guide is an Astro Starlight site published independently at
`docs.slipramp.com`. Guide content lives in `src/content/docs` and should use
plain Markdown unless a page needs an interactive Starlight component.

## Authoring

Every page needs a title:

```md
---
title: Page title
---

Write the page here.
```

Folders under `src/content/docs` become URL segments. Navigation order and
groups are configured in `astro.config.mjs`. Use root-relative links between
guide pages, for example `[Run an action](/guides/run-an-action/)`.

Place product screenshots in `src/assets/screenshots` and reference them with a
relative Markdown image path. Use descriptive alt text and crop screenshots to
the UI relevant to the instruction.

Fenced code blocks support syntax highlighting, titles, line highlighting, and
diffs:

````md
```yaml title=".slipramp/catalog.yaml" {2}
apiVersion: slipramp.com/v1alpha1
kind: Target
```
````

Set `draft: true` in page frontmatter to keep unfinished content out of a
production build.

## Development

- `task docs:dev` starts the local authoring server.
- `task docs:build` checks and builds the static site.
- `task docs:deploy` builds and deploys the `slipramp-docs` Worker.

Attach `docs.slipramp.com` to the Worker once in Cloudflare. Routine deploys do
not manage the zone binding.
