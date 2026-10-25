import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { createLogger, defineConfig, type Logger, type Plugin } from "vite";

function buildLogger(): Logger {
  const logger = createLogger();
  const warn = logger.warn.bind(logger);

  logger.warn = (message, options) => {
    const ignoredClientDirective =
      message.includes("[MODULE_LEVEL_DIRECTIVE]") &&
      message.includes("node_modules/.pnpm/@mantine+");
    if (!ignoredClientDirective) warn(message, options);
  };

  return logger;
}

function cloudflareHeaders(): Plugin {
  return {
    generateBundle() {
      this.emitFile({
        fileName: "_headers",
        source: readFileSync(new URL("./mascot/_headers", import.meta.url), "utf8"),
        type: "asset",
      });
    },
    name: "cloudflare-headers",
  };
}

export default defineConfig({
  build: {
    emptyOutDir: true,
    outDir: fileURLToPath(new URL("./dist/mascot", import.meta.url)),
  },
  customLogger: buildLogger(),
  plugins: [cloudflareHeaders()],
  publicDir: fileURLToPath(new URL("./public", import.meta.url)),
  resolve: {
    alias: {
      "~": fileURLToPath(new URL("./app", import.meta.url)),
    },
  },
  root: fileURLToPath(new URL("./mascot", import.meta.url)),
});
