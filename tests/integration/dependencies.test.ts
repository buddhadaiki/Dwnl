import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync } from "node:fs";
import path from "node:path";
import { expect, test } from "vitest";

test("private dependency probes pass their exact version flags and produce a real MP3", () => {
  const project = process.cwd();
  const temporary = path.join(project, ".tools/tmp");
  mkdirSync(temporary, { recursive: true });
  const output = mkdtempSync(path.join(temporary, "dwnl-package-probe-"));
  const quote = (value: string) => `'${value.replaceAll("'", "''")}'`;
  execFileSync("pwsh.exe", ["-NoProfile", "-NonInteractive", "-Command", [
    "$ErrorActionPreference='Stop'",
    `. ${quote(path.join(project, "tools/package.ps1"))}`,
    `$dependencyLock=Get-Content -LiteralPath ${quote(path.join(project, "release/dependencies.lock.json"))} -Raw | ConvertFrom-Json`,
    `Assert-DwnlMediaCapabilities -ProjectRoot ${quote(project)} -Lock $dependencyLock -OutputRoot ${quote(output)}`,
  ].join("\n")], { cwd: project, windowsHide: true, encoding: "utf8", timeout: 20000 });
  const metadata = JSON.parse(readFileSync(path.join(output, "capability-readback.json"), "utf8").replace(/^\uFEFF/, ""));
  expect(metadata.streams[0].codec_name).toBe("mp3");
  expect(metadata.streams[0].bit_rate).toBe("192000");
  expect(Number(metadata.format.duration)).toBeGreaterThan(0);
}, 25000);
