import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { createLogger, defineConfig, type Logger, type Plugin } from "vite";

function buildLogger(): Logger {
  const logger = createLogger();
  const warn = logger.warn.bind(logger);

  logger.warn = (message, options) => {
    const ignoredClientDirective =
      message.includes("[MODULE_LEVEL_DIRECTIVE]") &&
      message.includes("node_modules/.pnpm/@fontsource-variable+");
    if (!ignoredClientDirective) warn(message, options);
  };

  return logger;
}

function cloudflareHeaders(): Plugin {
  return {
    generateBundle() {
      this.emitFile({
        fileName: "_headers",
        source: readFileSync(new URL("./site/_headers", import.meta.url), "utf8"),
        type: "asset",
      });
    },
    name: "cloudflare-headers",
  };
}

export default defineConfig({
  build: {
    emptyOutDir: true,
    outDir: fileURLToPath(new URL("./dist/site", import.meta.url)),
    rollupOptions: {
      input: {
        home: fileURLToPath(new URL("./site/index.html", import.meta.url)),
        privacy: fileURLToPath(new URL("./site/privacy/index.html", import.meta.url)),
      },
    },
  },
  customLogger: buildLogger(),
  plugins: [cloudflareHeaders()],
  publicDir: fileURLToPath(new URL("./public", import.meta.url)),
  root: fileURLToPath(new URL("./site", import.meta.url)),
});
