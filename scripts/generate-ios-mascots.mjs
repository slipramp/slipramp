#!/usr/bin/env node

import { execFile as execFileCallback } from "node:child_process";
import { createHash } from "node:crypto";
import {
  access,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rename,
  rm,
  stat,
  unlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath, pathToFileURL } from "node:url";

const execFile = promisify(execFileCallback);
const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, "..");
const webMascotDirectory = path.join(
  repositoryRoot,
  "web/app/features/mascot",
);
const sourceAssetDirectory = path.join(webMascotDirectory, "assets");
const destinationDirectory = path.join(
  repositoryRoot,
  "ios/Slipramp/Resources/Mascots",
);
const generatedSwiftPath = path.join(
  repositoryRoot,
  "ios/Slipramp/Domain/MascotCatalog.generated.swift",
);
const assetManifestPath = path.join(destinationDirectory, "manifest.json");
const executionActivityPath = path.join(
  webMascotDirectory,
  "execution-activity.ts",
);
const animationStylesPath = path.join(
  webMascotDirectory,
  "kawaii-activity.module.css",
);

const encoding = {
  alphaQuality: 100,
  method: 6,
  quality: 90,
};
const checkOnly = process.argv.includes("--check");

if (process.argv.some((argument) => argument !== "--check" && argument.startsWith("--"))) {
  throw new Error("usage: generate-ios-mascots.mjs [--check]");
}

const executionActivity = await import(pathToFileURL(executionActivityPath));
const pools = executionActivity.executionAnimationPosePools;
const css = await readFile(animationStylesPath, "utf8");
const definitions = parsePoseDefinitions(css, pools);
const generatedSwift = renderSwiftCatalog(definitions, pools);

if (checkOnly) {
  await checkGeneratedSwift(generatedSwift);
  await checkGeneratedAssets(definitions);
  console.log(
    `checked ${definitions.size} generated iOS mascot loops and their Swift catalog`,
  );
} else {
  await mkdir(destinationDirectory, { recursive: true });
  await writeFile(generatedSwiftPath, generatedSwift);
  await generateAssets(definitions);
}

function parsePoseDefinitions(styles, posePools) {
  const defaultAssetName = requiredMatch(
    styles,
    /\.sprite\s*\{[\s\S]*?background-image:\s+url\("\.\/assets\/([^"/]+\.webp)"\)/,
    "default sprite asset",
  );
  const defaultDuration = matchSeconds(
    styles.match(/\.sprite\s*\{[\s\S]*?animation:\s+[\w-]+\s+([0-9.]+s)/)?.[1],
    "default sprite duration",
  );
  const defaultAnimation = requiredMatch(
    styles,
    /\.sprite\s*\{[\s\S]*?animation:\s+([\w-]+)\s+[0-9.]+s/,
    "default sprite animation",
  );
  const blocks = new Map();
  const blockPattern = /\.sprite\[data-pose="([^"]+)"\]\s*\{([\s\S]*?)\n\}/g;

  for (const match of styles.matchAll(blockPattern)) {
    blocks.set(match[1], match[2]);
  }
  blocks.set(
    defaultAssetName.replace(/^slipramp-axolotl-|\.webp$/g, ""),
    `background-image: url("./assets/${defaultAssetName}");`,
  );

  const poseNames = [...new Set(Object.values(posePools).flat())];
  const result = new Map();

  for (const name of poseNames) {
    const block = blocks.get(name);
    if (!block) {
      throw new Error(`missing CSS sprite definition for ${name}`);
    }

    const assetName = requiredMatch(
      block,
      /background-image:\s+url\("\.\/assets\/([^"/]+\.webp)"\)/,
      `${name} asset`,
    );
    const expectedAssetName = `slipramp-axolotl-${name}.webp`;
    if (assetName !== expectedAssetName) {
      throw new Error(
        `${name} references ${assetName}; expected ${expectedAssetName}`,
      );
    }

    const durationMatch = block.match(/animation-duration:\s*([0-9.]+s)/);
    const duration = durationMatch
      ? matchSeconds(durationMatch[1], `${name} duration`)
      : defaultDuration;
    const animation =
      block.match(/animation-name:\s*([\w-]+)/)?.[1] ?? defaultAnimation;
    const scale = Number(block.match(/--sprite-scale:\s*([0-9.]+)/)?.[1] ?? 1);
    const frameStarts = parseFrameStarts(styles, animation);

    result.set(name, {
      animation,
      assetName,
      duration,
      frameStarts,
      name,
      scale,
    });
  }

  for (const name of blocks.keys()) {
    if (!result.has(name)) {
      throw new Error(`CSS defines ${name}, but no execution animation pool uses it`);
    }
  }

  return result;
}

function parseFrameStarts(styles, name) {
  const marker = `@keyframes ${name} {`;
  const markerIndex = styles.indexOf(marker);
  if (markerIndex < 0) {
    throw new Error(`missing @keyframes ${name}`);
  }

  const openBraceIndex = styles.indexOf("{", markerIndex);
  let depth = 0;
  let closeBraceIndex = -1;

  for (let index = openBraceIndex; index < styles.length; index++) {
    if (styles[index] === "{") depth++;
    if (styles[index] === "}") depth--;
    if (depth === 0) {
      closeBraceIndex = index;
      break;
    }
  }

  if (closeBraceIndex < 0) {
    throw new Error(`unterminated @keyframes ${name}`);
  }

  const block = styles.slice(openBraceIndex + 1, closeBraceIndex);
  const starts = [
    0,
    matchFramePercentage(block, /calc\(100% \/ 3\)/, name),
    matchFramePercentage(block, /calc\(200% \/ 3\)/, name),
    matchFramePercentage(block, /100%/, name),
  ];

  if (starts.some((value, index) => index > 0 && value <= starts[index - 1])) {
    throw new Error(`${name} frame starts must be strictly increasing: ${starts}`);
  }
  if (starts.at(-1) >= 1) {
    throw new Error(`${name} final frame must start before the loop ends`);
  }

  return starts;
}

function matchFramePercentage(block, positionPattern, animationName) {
  const pattern = new RegExp(
    `([0-9.]+)%\\s*\\{\\s*background-position-x:\\s*${positionPattern.source}\\s*;`,
  );
  const match = block.match(pattern);
  if (!match) {
    throw new Error(`could not find a frame threshold in @keyframes ${animationName}`);
  }
  return Number(match[1]) / 100;
}

function matchSeconds(value, label) {
  if (!value?.endsWith("s")) {
    throw new Error(`could not read ${label}`);
  }
  const seconds = Number(value.slice(0, -1));
  if (!Number.isFinite(seconds) || seconds <= 0) {
    throw new Error(`invalid ${label}: ${value}`);
  }
  return seconds;
}

function requiredMatch(value, pattern, label) {
  const match = value.match(pattern);
  if (!match) {
    throw new Error(`could not read ${label}`);
  }
  return match[1];
}

function renderSwiftCatalog(poseDefinitions, posePools) {
  const animationCases = Object.keys(posePools)
    .map((animation) => `    case ${swiftIdentifier(animation)} = "${animation}"`)
    .join("\n");
  const definitionLines = [...poseDefinitions.values()]
    .map(
      (definition) => `        "${definition.name}": MascotPoseDefinition(
            name: "${definition.name}",
            duration: ${formatNumber(definition.duration)},
            frameStarts: [${definition.frameStarts.map(formatNumber).join(", ")}],
            scale: ${formatNumber(definition.scale)}
        ),`,
    )
    .join("\n");
  const poolLines = Object.entries(posePools)
    .map(
      ([animation, names]) => `        .${swiftIdentifier(animation)}: [
${names.map((name) => `            "${name}",`).join("\n")}
        ],`,
    )
    .join("\n");

  return `// Generated by scripts/generate-ios-mascots.mjs. DO NOT EDIT.

import Foundation

enum MascotAnimation: String, CaseIterable, Equatable, Hashable, Sendable {
${animationCases}
}

struct MascotPoseDefinition: Equatable, Sendable {
    let name: String
    let duration: TimeInterval
    let frameStarts: [Double]
    let scale: Double
}

enum MascotCatalog {
    static let poseCount = ${poseDefinitions.size}

    private static let definitions: [String: MascotPoseDefinition] = [
${definitionLines}
    ]

    private static let poolNames: [MascotAnimation: [String]] = [
${poolLines}
    ]

    static func poses(for animation: MascotAnimation) -> [MascotPoseDefinition] {
        guard let names = poolNames[animation] else {
            preconditionFailure("Missing mascot pool for \\(animation.rawValue)")
        }

        return names.map { name in
            guard let definition = definitions[name] else {
                preconditionFailure("Missing mascot definition for \\(name)")
            }
            return definition
        }
    }
}
`;
}

function swiftIdentifier(value) {
  return value.replace(/-([a-z])/g, (_, character) => character.toUpperCase());
}

function formatNumber(value) {
  return Number(value.toFixed(9)).toString();
}

async function generateAssets(poseDefinitions) {
  await assertExecutable("dwebp");
  await assertExecutable("cwebp");

  const temporaryDirectory = await mkdtemp(
    path.join(tmpdir(), "slipramp-ios-mascots."),
  );

  try {
    const definitions = [...poseDefinitions.values()];
    await runConcurrent(definitions, 4, async (definition) => {
      const sourcePath = path.join(sourceAssetDirectory, definition.assetName);
      const decodedPath = path.join(temporaryDirectory, `${definition.name}.png`);
      const encodedPath = path.join(temporaryDirectory, definition.assetName);
      const outputPath = path.join(destinationDirectory, definition.assetName);

      await access(sourcePath);
      await execFile("dwebp", [sourcePath, "-quiet", "-o", decodedPath]);
      await assertPNGDimensions(decodedPath, 1_152, 288);
      await execFile("cwebp", [
        "-quiet",
        "-q",
        String(encoding.quality),
        "-alpha_q",
        String(encoding.alphaQuality),
        "-m",
        String(encoding.method),
        "-metadata",
        "none",
        decodedPath,
        "-o",
        encodedPath,
      ]);
      await rename(encodedPath, outputPath);
    });

    const expectedAssets = new Set(
      definitions.map((definition) => definition.assetName),
    );
    for (const entry of await readdir(destinationDirectory)) {
      if (entry.endsWith(".webp") && !expectedAssets.has(entry)) {
        await unlink(path.join(destinationDirectory, entry));
      }
    }

    const manifest = await createAssetManifest(definitions);
    await writeFile(assetManifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
    const totalBytes = manifest.assets.reduce((total, asset) => total + asset.bytes, 0);
    console.log(
      `generated ${definitions.length} iOS mascot loops (${formatBytes(totalBytes)})`,
    );
  } finally {
    await rm(temporaryDirectory, { force: true, recursive: true });
  }
}

async function createAssetManifest(definitions) {
  const assets = [];

  for (const definition of definitions) {
    const sourcePath = path.join(sourceAssetDirectory, definition.assetName);
    const outputPath = path.join(destinationDirectory, definition.assetName);
    const outputStat = await stat(outputPath);

    assets.push({
      bytes: outputStat.size,
      name: definition.assetName,
      outputSHA256: await sha256(outputPath),
      sourceSHA256: await sha256(sourcePath),
    });
  }

  return {
    assets,
    encoding,
    frameCount: 4,
    frameSize: 288,
    version: 1,
  };
}

async function checkGeneratedSwift(expected) {
  const actual = await readFile(generatedSwiftPath, "utf8").catch(() => undefined);
  if (actual !== expected) {
    throw new Error(
      "the generated Swift mascot catalog is stale; run task mascot:generate",
    );
  }
}

async function checkGeneratedAssets(poseDefinitions) {
  const manifest = JSON.parse(await readFile(assetManifestPath, "utf8"));
  if (JSON.stringify(manifest.encoding) !== JSON.stringify(encoding)) {
    throw new Error("the iOS mascot encoding settings are stale");
  }
  if (manifest.frameCount !== 4 || manifest.frameSize !== 288) {
    throw new Error("the iOS mascot sprite contract is stale");
  }

  const expectedNames = [...poseDefinitions.values()].map(
    (definition) => definition.assetName,
  );
  const manifestNames = manifest.assets.map((asset) => asset.name);
  if (JSON.stringify(manifestNames) !== JSON.stringify(expectedNames)) {
    throw new Error("the iOS mascot asset manifest does not match the web catalog");
  }

  for (const asset of manifest.assets) {
    const sourcePath = path.join(sourceAssetDirectory, asset.name);
    const outputPath = path.join(destinationDirectory, asset.name);
    const outputStat = await stat(outputPath).catch(() => undefined);
    if (!outputStat || outputStat.size !== asset.bytes) {
      throw new Error(`${asset.name} is missing or has the wrong size`);
    }
    if ((await sha256(sourcePath)) !== asset.sourceSHA256) {
      throw new Error(`${asset.name} was generated from a stale web asset`);
    }
    if ((await sha256(outputPath)) !== asset.outputSHA256) {
      throw new Error(`${asset.name} does not match its generated checksum`);
    }
  }

  const generatedNames = (await readdir(destinationDirectory))
    .filter((entry) => entry.endsWith(".webp"))
    .toSorted();
  if (JSON.stringify(generatedNames) !== JSON.stringify(expectedNames.toSorted())) {
    throw new Error("the iOS mascot resource directory contains unexpected assets");
  }
}

async function sha256(filePath) {
  return createHash("sha256").update(await readFile(filePath)).digest("hex");
}

async function assertPNGDimensions(filePath, expectedWidth, expectedHeight) {
  const png = await readFile(filePath);
  const signature = png.subarray(0, 8).toString("hex");
  const width = png.readUInt32BE(16);
  const height = png.readUInt32BE(20);

  if (
    signature !== "89504e470d0a1a0a" ||
    width !== expectedWidth ||
    height !== expectedHeight
  ) {
    throw new Error(
      `${path.basename(filePath)} must be ${expectedWidth}x${expectedHeight}, got ${width}x${height}`,
    );
  }
}

async function assertExecutable(command) {
  await execFile(command, ["-version"]).catch((error) => {
    throw new Error(`${command} is required to generate iOS mascot assets`, {
      cause: error,
    });
  });
}

async function runConcurrent(values, concurrency, operation) {
  let nextIndex = 0;

  async function worker() {
    while (nextIndex < values.length) {
      const value = values[nextIndex];
      nextIndex++;
      await operation(value);
    }
  }

  await Promise.all(
    Array.from({ length: Math.min(concurrency, values.length) }, worker),
  );
}

function formatBytes(bytes) {
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}
