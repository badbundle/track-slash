import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outputDir = resolve(root, "internal/server/static");

const fromRoot = (path) => resolve(root, path);

mkdirSync(outputDir, { recursive: true });

const tailwind = fromRoot(process.platform === "win32" ? "node_modules/.bin/tailwindcss.cmd" : "node_modules/.bin/tailwindcss");
execFileSync(tailwind, [
  "--config", fromRoot("tailwind.config.cjs"),
  "--input", fromRoot("frontend/tailwind.css"),
  "--output", resolve(outputDir, "app.css"),
  "--minify",
], { stdio: "inherit" });

copyFileSync(fromRoot("node_modules/htmx.org/dist/htmx.min.js"), resolve(outputDir, "htmx.min.js"));
copyFileSync(fromRoot("node_modules/lucide/dist/umd/lucide.min.js"), resolve(outputDir, "lucide.min.js"));
copyFileSync(fromRoot("frontend/preload.js"), resolve(outputDir, "preload.js"));

const bundledPackages = ["htmx.org", "lucide", "tailwindcss"];
const licenses = bundledPackages.map((packageName) => {
  const packageDir = fromRoot(`node_modules/${packageName}`);
  const packageMetadata = JSON.parse(readFileSync(resolve(packageDir, "package.json"), "utf8"));
  const license = readFileSync(resolve(packageDir, "LICENSE"), "utf8").trim();
  return `${packageMetadata.name} ${packageMetadata.version}\n${license}`;
});
writeFileSync(resolve(outputDir, "THIRD_PARTY_LICENSES.txt"), `${licenses.join("\n\n---\n\n")}\n`);
