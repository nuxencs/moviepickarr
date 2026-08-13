import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const repoRoot = resolve(import.meta.dir, "../..");
const stateDir = await mkdtemp(join(tmpdir(), "moviepickarr-e2e-"));
const dbFile = join(stateDir, "moviepickarr.db");
const binary = join(stateDir, "moviepickarr");
const env = {
  ...process.env,
  DB_BACKUP_MAX: "0",
  DB_FILE: dbFile,
  MPA_ADMIN_NAME: "",
  MPA_ADMIN_PASSWORD: "",
  MPA_ADMIN_USERNAME: "",
  TMDB_API_KEY: "",
  TMDB_ENABLED: "false",
};

async function run(command: string[]) {
  const child = Bun.spawn(command, {
    cwd: repoRoot,
    env,
    stdout: "inherit",
    stderr: "inherit",
  });
  activeProcess = child;
  const exitCode = await child.exited;
  if (activeProcess === child) activeProcess = null;
  if (exitCode !== 0) throw new Error(`${command.join(" ")} exited with code ${exitCode}`);
}

let activeProcess: ReturnType<typeof Bun.spawn> | null = null;
let stopRequested = false;

function requestStop() {
  stopRequested = true;
  activeProcess?.kill("SIGTERM");
}

process.on("SIGINT", requestStop);
process.on("SIGTERM", requestStop);

try {
  await run(["go", "run", "./cmd/devfixtures"]);
  await run(["go", "build", "-o", binary, "."]);

  if (stopRequested) {
    process.exitCode = 130;
  } else {
    const server = Bun.spawn([binary], {
      cwd: repoRoot,
      env,
      stdout: "inherit",
      stderr: "inherit",
    });
    activeProcess = server;
    const exitCode = await server.exited;
    if (activeProcess === server) activeProcess = null;
    process.exitCode = stopRequested ? 130 : exitCode;
  }
} finally {
  if (activeProcess) {
    activeProcess.kill("SIGTERM");
    await activeProcess.exited;
    activeProcess = null;
  }
  process.off("SIGINT", requestStop);
  process.off("SIGTERM", requestStop);
  await rm(stateDir, { recursive: true, force: true });
}
