#!/usr/bin/env node

import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const platforms = {
  "darwin-arm64": "@fortrabbit/cli-darwin-arm64",
  "darwin-x64": "@fortrabbit/cli-darwin-x64",
  "linux-arm64": "@fortrabbit/cli-linux-arm64",
  "linux-x64": "@fortrabbit/cli-linux-x64",
  "win32-arm64": "@fortrabbit/cli-win32-arm64",
  "win32-x64": "@fortrabbit/cli-win32-x64"
};

const key = `${process.platform}-${process.arch}`;
const packageName = platforms[key];

if (!packageName) {
  console.error(`frbit does not support ${process.platform}/${process.arch}`);
  process.exit(1);
}

let packagePath;
try {
  packagePath = require.resolve(`${packageName}/package.json`);
} catch {
  console.error(
    `The optional package ${packageName} is missing. ` +
      "Reinstall @fortrabbit/cli without omitting optional dependencies."
  );
  process.exit(1);
}

const binary = join(dirname(packagePath), "bin", process.platform === "win32" ? "frbit.exe" : "frbit");
const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`Unable to run frbit: ${result.error.message}`);
  process.exit(1);
}

if (result.signal) {
  process.kill(process.pid, result.signal);
}

process.exit(result.status ?? 1);
