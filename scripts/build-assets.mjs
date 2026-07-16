import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outputDir = resolve(root, "internal/server/static");

const fromRoot = (path) => resolve(root, path);

function extractTemplateScript(path, templateName) {
  const source = readFileSync(fromRoot(path), "utf8");
  const marker = `{{define "${templateName}"}}`;
  const markerIndex = source.indexOf(marker);
  if (markerIndex < 0) throw new Error(`${path}: missing ${marker}`);

  const match = source.slice(markerIndex + marker.length).match(/<script>\s*\n([\s\S]*?)\n\s*<\/script>/);
  if (!match) throw new Error(`${path}: missing script for ${templateName}`);

  const lines = match[1].split("\n");
  const indents = lines.filter((line) => line.trim()).map((line) => line.match(/^\s*/)[0].length);
  const indent = Math.min(...indents);
  return `${lines.map((line) => line.slice(Math.min(indent, line.length))).join("\n")}\n`;
}

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
writeFileSync(resolve(outputDir, "app.js"), extractTemplateScript("internal/server/templates/shell_scripts.html", "shell-scripts"));
writeFileSync(resolve(outputDir, "auth.js"), extractTemplateScript("internal/server/templates/login.html", "auth-passkey-scripts"));

const bundledPackages = ["htmx.org", "lucide", "tailwindcss"];
const licenses = bundledPackages.map((packageName) => {
  const packageDir = fromRoot(`node_modules/${packageName}`);
  const packageMetadata = JSON.parse(readFileSync(resolve(packageDir, "package.json"), "utf8"));
  const license = readFileSync(resolve(packageDir, "LICENSE"), "utf8").trim();
  return `${packageMetadata.name} ${packageMetadata.version}\n${license}`;
});
writeFileSync(resolve(outputDir, "THIRD_PARTY_LICENSES.txt"), `${licenses.join("\n\n---\n\n")}\n`);
