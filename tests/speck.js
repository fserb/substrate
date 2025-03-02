#!/bin/sh
// 2>/dev/null; exec deno test -A "$0" -- "$@"; exit

import { dirname, join } from "jsr:@std/path@^1.0.8";
import { bold, cyan, red, yellow } from "jsr:@std/fmt@^1.0.5/colors";

const basePath = import.meta.dirname;
let verbose = false;
let tmpDir = "";
let activeTests = 0;
let cleanup = null;

async function run(cmd, sync = false) {
  const proc = new Deno.Command("bash", {
    args: ["-c", cmd],
    env: {
      PATH: `${basePath}/_lib:${Deno.env.get("PATH")}`,
      DIR: basePath,
    },
    stderr: "inherit",
  });

  const { code, stdout } = sync ? proc.outputSync() : await proc.output();
  const output = new TextDecoder().decode(stdout).trim();
  return code === 0 ? output : `status: ${code}\n${output}`;
}

async function setupTmpDir() {
  const currentDir = Deno.cwd();
  tmpDir = await Deno.makeTempDir({ prefix: "test-" });

  await run(`cp -R ${basePath}/* ${tmpDir}`);
  Deno.chdir(tmpDir);

  let hasShutdown = false;
  return () => {
    if (hasShutdown) return;
    Deno.chdir(currentDir);
    run(`rm -rf ${tmpDir}`, true);
    hasShutdown = true;
  };
}

async function parseTestFile(file) {
  const lines = (await Deno.readTextFile(file)).trim().split("\n");
  const commands = [];

  let cmd = "", output = [], lineNum = 0, cmdLine = 0;

  function saveCommand() {
    if (cmd) {
      commands.push({ cmd, expected: output.join("\n"), line: cmdLine });
      cmd = "";
      output = [];
    }
  }

  for (const line of lines) {
    lineNum++;
    if (line.startsWith("#")) continue;

    const match = line.match(/^\s*\$ (.+)$/);
    if (match) {
      saveCommand();
      cmd = match[1];
      cmdLine = lineNum;
      continue;
    }

    if (cmd) output.push(line);
  }

  saveCommand();
  return commands;
}

async function generateDiff(actual, expected) {
  const escapeStr = (s) => s.replaceAll('"', '\\"').replaceAll("$", "\\$");
  const diffCmd = `diff --color=always -U 3 <(echo "${
    escapeStr(actual)
  }") <(echo "${escapeStr(expected)}") | tail -n +4`;
  return await run(diffCmd);
}

function wrapTest(fn) {
  activeTests++;

  return async () => {
    try {
      await fn();
    } finally {
      activeTests--;
      if (activeTests === 0 && cleanup) {
        cleanup();
      }
    }
  };
}

function setupTestsFromFile(file, range = []) {
  Deno.test(
    `Test ${file}`,
    wrapTest(async () => {
      const testDir = dirname(join(tmpDir, file));
      const testFile = join(tmpDir, file);
      const commands = await parseTestFile(testFile);

      if (verbose) console.log(bold(`Testing ${file}`));

      try {
        Deno.chdir(testDir);

        for (let i = 0; i < commands.length; i++) {
          if (range.length > 0 && i < range[0]) continue;
          if (range.length > 1 && i >= (range[0] + range[1])) break;

          const { cmd, expected, line } = commands[i];
          if (verbose) console.log("" + line, cyan(`${i}$ ${cmd}`));

          const output = await run(cmd);
          if (verbose) console.log(output);

          if (expected !== "*" && output !== expected) {
            const diff = await generateDiff(output, expected);
            throw new Error([
              `${red("✗")} Command ${bold(i)} at line ${line} failed:`,
              `  $ ${cyan(cmd)}`,
              `  ${yellow("Diff:")}`,
              diff.split("\n").map((line) => `  ${line}`).join("\n"),
            ].join("\n"));
          }
        }
      } finally {
        Deno.chdir(tmpDir);
      }
    }),
  );
}

addEventListener("beforeunload", () => {
  if (cleanup) cleanup();
});

async function main() {
  const tests = [];

  for (const arg of Deno.args) {
    if (arg === "-v") {
      verbose = true;
      continue;
    }

    const parts = arg.split(":");
    tests.push({
      file: parts[0],
      range: parts.slice(1).map(Number),
    });
  }

  if (tests.length === 0) {
    console.log(
      "Usage: deno test test_framework.js -- [-v] path/to/testfile.sh[:start[:count]] ...",
    );
    Deno.exit(1);
  }

  cleanup = await setupTmpDir();
  for (const { file, range } of tests) {
    setupTestsFromFile(file, range);
  }
}
await main();

