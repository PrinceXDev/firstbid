// Copies the static export into the Go command so it can be go:embed-ed.
// This is the only step where Node touches the deliverable; nothing JavaScript
// runs at serve time.
import { cp, rm, mkdir, readdir } from "node:fs/promises";
import { existsSync } from "node:fs";

const from = new URL("../out/", import.meta.url);
const to = new URL("../../core/cmd/dashboard/dist/", import.meta.url);

if (!existsSync(from)) {
  console.error("no ./out — run `next build` first");
  process.exit(1);
}
await rm(to, { recursive: true, force: true });
await mkdir(to, { recursive: true });
await cp(from, to, { recursive: true });
const files = await readdir(to, { recursive: true });
console.log(`embedded ${files.length} files into core/cmd/dashboard/dist`);
