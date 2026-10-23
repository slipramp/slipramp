# Brand and mascot assets

Slipramp's canonical source artwork, prompts, prototypes, explorations, and export masters are maintained outside this repository in the sibling `../slipramp-assets` repository. The standard local checkout is `~/Workspace/slipramp-assets`.

This repository intentionally contains only the optimized production outputs required for clean Web and iOS builds:

- `web/public/` contains exported brand icons.
- `web/app/features/mascot/assets/` contains production Web sprites.
- `ios/Slipramp/Resources/` contains native brand and mascot resources.
- Mascot catalogs, presentation code, packaging scripts, and generated Swift code remain with the application.

Do not add a top-level `assets/` directory, source sheets, prompt archives, or design explorations here. Work from the sibling asset repository, export only the production files the application needs, and verify the Web gallery, native catalog, reduced-motion behavior, and ordinary application builds. The application repository must remain fully buildable when the sibling checkout is absent.
