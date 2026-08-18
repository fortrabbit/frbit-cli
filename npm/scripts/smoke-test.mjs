import { mkdir, rm, symlink } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const npmRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const suffixes = {
  "darwin-arm64": "darwin-arm64",
  "darwin-x64": "darwin-x64",
  "linux-arm64": "linux-arm64",
  "linux-x64": "linux-x64",
  "win32-arm64": "win32-arm64",
  "win32-x64": "win32-x64"
};
const suffix = suffixes[`${process.platform}-${process.arch}`];
if (!suffix) throw new Error(`unsupported test platform ${process.platform}/${process.arch}`);

const packageName = `cli-${suffix}`;
const scopeRoot = join(npmRoot, "stage", "cli", "node_modules", "@fortrabbit");
const linkPath = join(scopeRoot, packageName);
await mkdir(scopeRoot, { recursive: true });
await rm(linkPath, { recursive: true, force: true });
await symlink(join(npmRoot, "stage", suffix), linkPath, process.platform === "win32" ? "junction" : "dir");

const result = spawnSync(process.execPath, [join(npmRoot, "stage", "cli", "bin", "frbit.js"), "version"], {
  encoding: "utf8"
});
if (result.status !== 0) {
  process.stderr.write(result.stderr);
  process.exit(result.status ?? 1);
}
if (!result.stdout.startsWith("frbit ")) {
  throw new Error(`unexpected launcher output: ${result.stdout}`);
}

process.stdout.write(result.stdout);
