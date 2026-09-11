import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

if (process.platform !== "darwin") {
  throw new Error("DMG packaging requires macOS");
}

const root = resolve(fileURLToPath(new URL("../..", import.meta.url)));
const config = JSON.parse(
  await readFile(join(root, "desktop", "src-tauri", "tauri.conf.json"), "utf8"),
);
const helpDocuments = JSON.parse(
  await readFile(join(root, "web", "src", "helpDocuments.json"), "utf8"),
);
const architecture = process.arch === "arm64" ? "aarch64" : process.arch;
const bundleRoot = join(root, "desktop", "src-tauri", "target", "release", "bundle");
const appPath = join(bundleRoot, "macos", `${config.productName}.app`);
const dmgDirectory = join(bundleRoot, "dmg");
const dmgPath = join(dmgDirectory, `${config.productName}_${config.version}_${architecture}.dmg`);
const staging = await mkdtemp(join(tmpdir(), "jarvis-dmg-"));
const validateRuntime = join(root, "packaging", "macos", "validate-runtime.sh");

function run(command, args) {
  execFileSync(command, args, { stdio: "inherit" });
}

try {
  run("zsh", [
    validateRuntime,
    join(appPath, "Contents", "Resources", "runtime"),
    appPath,
  ]);
  run("codesign", ["--force", "--deep", "--sign", "-", appPath]);
  run("codesign", ["--verify", "--deep", "--strict", "--verbose=2", appPath]);
  // Archive the same final signed app that is copied into the DMG below.
  const updaterPath = join(bundleRoot, "macos", `${config.productName}.app.tar.gz`);
  await rm(`${updaterPath}.sig`, { force: true });
  run("tar", ["-czf", updaterPath, "-C", join(bundleRoot, "macos"), `${config.productName}.app`]);
  await mkdir(dmgDirectory, { recursive: true });
  await rm(dmgPath, { force: true });
  run("ditto", [appPath, join(staging, `${config.productName}.app`)]);
  await symlink("/Applications", join(staging, "Applications"));
  for (const document of helpDocuments) {
    const url = document.url.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
    await writeFile(
      join(staging, `${document.title}.webloc`),
      `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict><key>URL</key><string>${url}</string></dict></plist>\n`,
    );
  }
  run("hdiutil", [
    "create",
    "-srcfolder",
    staging,
    "-volname",
    config.productName,
    "-fs",
    "HFS+",
    "-format",
    "UDZO",
    "-ov",
    "-imagekey",
    "zlib-level=9",
    dmgPath,
  ]);
  run("hdiutil", ["verify", dmgPath]);
  console.log(`dmg=${dmgPath}`);
} finally {
  await rm(staging, { recursive: true, force: true });
}
