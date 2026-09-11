import { execFileSync } from "node:child_process";
import { createReadStream } from "node:fs";
import { createGunzip } from "node:zlib";

const BLOCK = 512;

// macOS tar stores extended attributes as separate AppleDouble members. The
// updater unpacks with a plain tar reader and fails on the first `._` entry,
// so the archive must not contain them. Every macOS tar listing mode merges
// those members back into their parent, which is why this reads headers directly.
export async function findAppleDoubleEntry(archivePath) {
  const stream = createReadStream(archivePath).pipe(createGunzip());
  let pending = Buffer.alloc(0);
  let skip = 0;
  for await (const chunk of stream) {
    let data = pending.length > 0 ? Buffer.concat([pending, chunk]) : chunk;
    pending = Buffer.alloc(0);
    let offset = 0;
    while (true) {
      if (skip > 0) {
        const dropped = Math.min(skip, data.length - offset);
        offset += dropped;
        skip -= dropped;
        if (skip > 0) {
          break;
        }
      }
      if (data.length - offset < BLOCK) {
        pending = Buffer.from(data.subarray(offset));
        break;
      }
      const header = data.subarray(offset, offset + BLOCK);
      offset += BLOCK;
      const name = header.subarray(0, 100).toString("utf8").replace(/\0.*$/s, "");
      if (name === "") {
        continue;
      }
      if (name.split("/").pop().startsWith("._")) {
        stream.destroy();
        return name;
      }
      const rawSize = header.subarray(124, 136).toString("utf8").replace(/\0.*$/s, "").trim();
      const size = rawSize === "" ? 0 : Number.parseInt(rawSize, 8);
      if (!Number.isFinite(size) || size < 0) {
        throw new Error(`unreadable tar header size for ${name} in ${archivePath}`);
      }
      skip = Math.ceil(size / BLOCK) * BLOCK;
    }
  }
  return null;
}

export async function createUpdaterArchive(directory, appName, archivePath) {
  execFileSync("tar", ["-czf", archivePath, "-C", directory, appName], {
    stdio: "inherit",
    env: { ...process.env, COPYFILE_DISABLE: "1" },
  });
  const appleDouble = await findAppleDoubleEntry(archivePath);
  if (appleDouble !== null) {
    throw new Error(`updater archive contains AppleDouble metadata: ${appleDouble}`);
  }
}
