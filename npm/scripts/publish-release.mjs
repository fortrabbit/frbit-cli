import { readFile, readdir } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const npmRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const stageRoot = join(npmRoot, "stage");
const dryRun = process.argv.includes("--dry-run");
const packages = (await readdir(stageRoot, { withFileTypes: true }))
  .filter((entry) => entry.isDirectory() && entry.name !== "cli")
  .map((entry) => join(stageRoot, entry.name))
  .sort();

// Publish the wrapper last so every referenced optional dependency already exists.
packages.push(join(stageRoot, "cli"));

for (const packagePath of packages) {
  const manifest = JSON.parse(await readFile(join(packagePath, "package.json"), "utf8"));
  const args = dryRun
    ? ["pack", packagePath, "--dry-run"]
    : ["publish", packagePath, "--access", "public"];
  if (!dryRun && manifest.version.includes("-")) args.push("--tag", "next");

  const result = spawnSync("npm", args, { stdio: "inherit" });
  if (result.status !== 0) process.exit(result.status ?? 1);
}
