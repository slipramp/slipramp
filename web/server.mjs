import path from "node:path";

import { createRequestHandler } from "@react-router/express";
import compression from "compression";
import express from "express";
import morgan from "morgan";

import { accessLogURL } from "./app/features/auth/access-log.mjs";

process.env.NODE_ENV ??= "production";

const build = await import("./build/server/index.js");
const host = process.env.HOST;
const port = parsePort(process.env.PORT);
const trustedProxyHops = parseTrustedProxyHops(process.env.SLIPRAMP_WEB_TRUST_PROXY_HOPS);

const app = express();
app.disable("x-powered-by");

if (trustedProxyHops > 0) {
  app.set("trust proxy", trustedProxyHops);
}

app.use(compression());

const publicPath = expressPath(build.publicPath);
app.use(
  path.posix.join(publicPath, "assets"),
  express.static(path.join(build.assetsBuildDirectory, "assets"), {
    immutable: true,
    maxAge: "1y",
  }),
);
app.use(publicPath, express.static(build.assetsBuildDirectory));
app.use(express.static("public", { maxAge: "1h" }));
app.use("/.well-known", express.static(path.join(build.assetsBuildDirectory, ".well-known")));
morgan.token("url", accessLogURL);
app.use(morgan("tiny"));
app.all(
  "/{*splat}",
  createRequestHandler({
    build,
    mode: process.env.NODE_ENV,
  }),
);

const onListen = (error) => {
  if (error) throw error;
  console.log(`[slipramp-web] http://${host ?? "localhost"}:${port}`);
};

const server = host ? app.listen(port, host, onListen) : app.listen(port, onListen);

for (const signal of ["SIGTERM", "SIGINT"]) {
  process.once(signal, () => server.close(console.error));
}

function expressPath(rawPublicPath) {
  let pathname;
  try {
    pathname = new URL(rawPublicPath).pathname;
  } catch {
    pathname = rawPublicPath;
  }

  return pathname.startsWith("/") ? pathname : `/${pathname}`;
}

function parsePort(raw) {
  if (raw === undefined) return 3000;

  const parsed = Number(raw);
  if (!Number.isSafeInteger(parsed) || parsed < 1 || parsed > 65_535) {
    throw new Error("PORT must be an integer between 1 and 65535");
  }

  return parsed;
}

function parseTrustedProxyHops(raw) {
  if (raw === undefined) return 0;

  const parsed = Number(raw);
  if (!Number.isSafeInteger(parsed) || parsed < 0) {
    throw new Error("SLIPRAMP_WEB_TRUST_PROXY_HOPS must be a nonnegative integer");
  }

  return parsed;
}
