import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const workspaceRoot = join(scriptDirectory, "..", "..");

main();

function main() {
  const packageJson = JSON.parse(
    readFileSync(join(workspaceRoot, "package.json"), "utf8")
  );
  const pnpmCommand = resolvePnpmCommand(packageJson.packageManager);

  run(pnpmCommand, ["install", "--frozen-lockfile", "--ignore-scripts"]);
  run([process.execPath], [join(scriptDirectory, "prewarm-electron.mjs")]);
  run(pnpmCommand, ["exec", "husky"]);
}

function resolvePnpmCommand(packageManager) {
  const match = /^pnpm@(.+)$/u.exec(String(packageManager ?? ""));
  if (!match) {
    throw new Error("package.json must declare a pnpm packageManager version");
  }

  return [
    process.platform === "win32" ? "corepack.cmd" : "corepack",
    `pnpm@${match[1]}`
  ];
}

function run(commandParts, argumentsList) {
  const [command, ...commandArguments] = commandParts;
  const result = spawnSync(command, [...commandArguments, ...argumentsList], {
    cwd: workspaceRoot,
    env: process.env,
    stdio: "inherit"
  });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
