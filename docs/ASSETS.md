# Brand and mascot assets

Slipramp's canonical source artwork, prompts, prototypes, explorations, and
export masters are maintained outside this repository in the sibling
`../slipramp-assets` repository. The standard local checkout is
`~/Workspace/slipramp-assets`.

This repository intentionally contains only the optimized production outputs
required for clean Web and iOS builds:

- `web/public/` contains exported brand icons.
- `web/app/features/mascot/assets/` contains production Web sprites.
- `ios/Slipramp/Resources/` contains native brand and mascot resources.
- Mascot catalogs, presentation code, packaging scripts, and generated Swift code remain with the application.

The Web catalog is the production source for lifecycle pose pools, display
metadata, and reviewed frame timing. Run and Promotion identities select poses
deterministically so presentation remains stable across refreshes. The native
catalog and optimized iOS resource pack are generated from that Web contract;
`task mascot:check` verifies that the checked-in outputs remain current.

The authenticated Web gallery and the independent public gallery exercise the
same production catalog for visual review. Their local and deployment workflows
are documented in [DEVELOPMENT.md](./DEVELOPMENT.md), while native usage and
accessibility rules live in the [iOS design system](../ios/DESIGN_SYSTEM.md).

Do not add a top-level `assets/` directory, source sheets, prompt archives, or
design explorations here. Work from the sibling asset repository, export only
the production files the application needs, and verify the Web gallery, native
catalog, reduced-motion behavior, and ordinary application builds. The
application repository must remain fully buildable when the sibling checkout is
absent.

## Licensing

The production artwork paths are identified in `LICENSE-ARTWORK.md` and licensed
under Creative Commons Attribution-ShareAlike 4.0 International. Preserve the
attribution and share-alike terms when exporting or adapting artwork from the
sibling repository. Copyright permission does not grant trademark permission;
follow `TRADEMARKS.md` when branding a fork or derivative.
