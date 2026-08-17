import { chmod, copyFile, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const npmRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(npmRoot, "..");
const stageRoot = join(npmRoot, "stage");
const version = process.argv[2];

if (!version || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error("usage: node npm/scripts/prepare-release.mjs <semver>");
}

const platforms = [
  { goos: "darwin", goarch: "arm64", nodeOs: "darwin", nodeCpu: "arm64", suffix: "darwin-arm64" },
  { goos: "darwin", goarch: "amd64", nodeOs: "darwin", nodeCpu: "x64", suffix: "darwin-x64" },
  { goos: "linux", goarch: "arm64", nodeOs: "linux", nodeCpu: "arm64", suffix: "linux-arm64" },
  { goos: "linux", goarch: "amd64", nodeOs: "linux", nodeCpu: "x64", suffix: "linux-x64" },
  { goos: "windows", goarch: "arm64", nodeOs: "win32", nodeCpu: "arm64", suffix: "win32-arm64" },
  { goos: "windows", goarch: "amd64", nodeOs: "win32", nodeCpu: "x64", suffix: "win32-x64" }
];

const artifacts = JSON.parse(await readFile(join(repositoryRoot, "dist", "artifacts.json"), "utf8"));
const wrapperTemplate = JSON.parse(await readFile(join(npmRoot, "package.json"), "utf8"));
const optionalDependencies = {};

await rm(stageRoot, { recursive: true, force: true });

for (const platform of platforms) {
  const artifact = artifacts.find(
    (item) => item.type === "Binary" && item.goos === platform.goos && item.goarch === platform.goarch
  );
  if (!artifact) {
    throw new Error(`missing GoReleaser binary for ${platform.goos}/${platform.goarch}`);
  }

  const packageName = `@fortrabbit/cli-${platform.suffix}`;
  const packageRoot = join(stageRoot, platform.suffix);
  const binaryName = platform.goos === "windows" ? "frbit.exe" : "frbit";
  const binaryPath = join(packageRoot, "bin", binaryName);
  const manifest = {
    name: packageName,
    version,
    description: `frbit native binary for ${platform.nodeOs}/${platform.nodeCpu}`,
    license: "MIT",
    os: [platform.nodeOs],
    cpu: [platform.nodeCpu],
    files: ["bin", "THIRD_PARTY_NOTICES"],
    repository: wrapperTemplate.repository,
    homepage: wrapperTemplate.homepage,
    publishConfig: { access: "public" }
  };

  await mkdir(dirname(binaryPath), { recursive: true });
  await copyFile(resolve(repositoryRoot, artifact.path), binaryPath);
  await chmod(binaryPath, 0o755);
  await copyFile(join(repositoryRoot, "LICENSE"), join(packageRoot, "LICENSE"));
  await copyFile(join(repositoryRoot, "THIRD_PARTY_NOTICES"), join(packageRoot, "THIRD_PARTY_NOTICES"));
  await writeFile(join(packageRoot, "package.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  optionalDependencies[packageName] = version;
}

const wrapperRoot = join(stageRoot, "cli");
const wrapperManifest = { ...wrapperTemplate, version, optionalDependencies };
await mkdir(join(wrapperRoot, "bin"), { recursive: true });
await copyFile(join(npmRoot, "bin", "frbit.js"), join(wrapperRoot, "bin", "frbit.js"));
await chmod(join(wrapperRoot, "bin", "frbit.js"), 0o755);
await copyFile(join(npmRoot, "README.md"), join(wrapperRoot, "README.md"));
await copyFile(join(repositoryRoot, "LICENSE"), join(wrapperRoot, "LICENSE"));
await copyFile(join(repositoryRoot, "THIRD_PARTY_NOTICES"), join(wrapperRoot, "THIRD_PARTY_NOTICES"));
await writeFile(join(wrapperRoot, "package.json"), `${JSON.stringify(wrapperManifest, null, 2)}\n`);

console.log(`Prepared @fortrabbit/cli ${version} and ${platforms.length} platform packages in npm/stage`);
