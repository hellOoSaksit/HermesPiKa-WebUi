// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Tests for install.sh / install.ps1: they must verify the SHA-256 before they
// run anything from a download, and fail with a readable message.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { spawnSync, spawn } from "node:child_process";
import { createServer } from "node:http";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from "node:fs";
import * as fs from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const win = process.platform === "win32";
const VER = "0.1.0";

const has = (cmd, args) => {
  const r = spawnSync(cmd, args, { encoding: "utf8" });
  return !r.error && r.status === 0;
};
const haveSh = has("sh", ["-c", "true"]) && has("sh", ["-c", "command -v tar"]);
const havePs = win && has("powershell", ["-NoProfile", "-Command", "exit 0"]);

let work, server, base;
const sha = (buf) => createHash("sha256").update(buf).digest("hex");

// Fake release dir: <work>/rel/{good,bad}/... served over http.
function build() {
  work = mkdtempSync(join(tmpdir(), "hpk-inst-"));
  const stage = join(work, "stage");
  mkdirSync(stage);
  writeFileSync(join(stage, "hpk"), '#!/bin/sh\necho "$@" >> "$STUB_LOG"\n');
  writeFileSync(join(stage, "hpk.exe"), "not a real exe");
  for (const sub of ["good", "bad"]) mkdirSync(join(work, "rel", sub), { recursive: true });
  const unixArch = ["linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"];
  if (haveSh) {
    for (const t of unixArch) {
      const name = `hpk_${VER}_${t}.tar.gz`;
      const out = join(work, "rel", "good", name);
      const r = spawnSync("sh", ["-c", `tar -czf - hpk > "$1"`, "sh", out], { cwd: stage });
      assert.equal(r.status, 0, String(r.stderr));
    }
  }
  if (havePs) {
    for (const a of ["amd64", "arm64"]) {
      const out = join(work, "rel", "good", `hpk_${VER}_windows_${a}.zip`);
      const r = spawnSync("powershell", ["-NoProfile", "-Command",
        `Compress-Archive -Path '${join(stage, "hpk.exe")}' -DestinationPath '${out}'`], { encoding: "utf8" });
      assert.equal(r.status, 0, r.stderr);
    }
  }
  // checksums.txt: right hashes in good/, wrong hashes in bad/.
  const { readdirSync, copyFileSync } = fs;
  for (const f of readdirSync(join(work, "rel", "good"))) {
    const buf = readFileSync(join(work, "rel", "good", f));
    copyFileSync(join(work, "rel", "good", f), join(work, "rel", "bad", f));
    appendLine(join(work, "rel", "good", "checksums.txt"), `${sha(buf)}  ${f}`);
    appendLine(join(work, "rel", "bad", "checksums.txt"), `${"0".repeat(64)}  ${f}`);
  }
}
const appendLine = (p, l) => fs.appendFileSync(p, l + "\n");

before(async () => {
  build();
  server = createServer((req, res) => {
    const p = join(work, "rel", decodeURIComponent(req.url.split("?")[0]));
    if (existsSync(p) && fs.statSync(p).isFile()) { res.end(readFileSync(p)); }
    else { res.statusCode = 404; res.end("nope"); }
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${server.address().port}`;
});
after(() => { server?.close(); if (work) rmSync(work, { recursive: true, force: true }); });

// The fake release server lives in this process, so the scripts must run
// asynchronously (spawnSync would freeze the server and deadlock).
function run(cmd, args, env, cwd) {
  return new Promise((resolve) => {
    const c = spawn(cmd, args, { env, cwd, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "", stderr = "";
    c.stdout.on("data", (d) => (stdout += d));
    c.stderr.on("data", (d) => (stderr += d));
    const t = setTimeout(() => c.kill(), 90000);
    c.on("close", (status) => { clearTimeout(t); resolve({ status, stdout, stderr }); });
  });
}

async function runSh(sub, extra = {}) {
  const log = join(work, `stub-${sub}.log`);
  rmSync(log, { force: true });
  // Git Bash reports MINGW; a fake uname makes the script see an ordinary Linux box.
  const fake = join(work, "fakebin");
  mkdirSync(fake, { recursive: true });
  writeFileSync(join(fake, "uname"), '#!/bin/sh\ncase "$1" in -m) echo x86_64;; *) echo Linux;; esac\n');
  spawnSync("sh", ["-c", 'chmod +x "$1"', "sh", join(fake, "uname").replaceAll("\\", "/")]);
  const sep = win ? ";" : ":";
  const r = await run("sh", [join(root, "install.sh").replaceAll("\\", "/")], {
    ...process.env, PATH: fake.replaceAll("\\", "/") + sep + process.env.PATH, HPK_VERSION: VER, HPK_RELEASE_BASE: `${base}/${sub}`, HPK_NO_START: "1",
    HPK_DATA_DIR: join(work, "data"), HPK_SELFINSTALL_DRYRUN: "1", STUB_LOG: log, ...extra,
  });
  return { ...r, log };
}

function runPs(sub, extra = {}) {
  return run("powershell", ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", join(root, "install.ps1")], {
    ...process.env, HPK_VERSION: VER, HPK_RELEASE_BASE: `${base}/${sub}`, HPK_NO_START: "1",
    HPK_DATA_DIR: join(work, "data-ps"), HPK_SELFINSTALL_DRYRUN: "1", ...extra,
  });
}

test("install.sh installs and calls self-install", { skip: !haveSh }, async () => {
  const r = await runSh("good");
  assert.equal(r.status, 0, r.stdout + r.stderr);
  assert.ok(existsSync(r.log), "stub was not run");
  assert.match(readFileSync(r.log, "utf8"), /self-install/);
});

test("install.sh refuses a bad checksum", { skip: !haveSh }, async () => {
  const r = await runSh("bad");
  assert.notEqual(r.status, 0);
  assert.match(r.stderr, /checksum/i);
  assert.ok(!existsSync(r.log), "stub ran despite bad checksum");
});

test("install.sh refuses an archive missing from checksums.txt", { skip: !haveSh }, async () => {
  mkdirSync(join(work, "rel", "nolist"), { recursive: true });
  for (const f of fs.readdirSync(join(work, "rel", "good"))) {
    if (f !== "checksums.txt") fs.copyFileSync(join(work, "rel", "good", f), join(work, "rel", "nolist", f));
  }
  writeFileSync(join(work, "rel", "nolist", "checksums.txt"), `${"1".repeat(64)}  something_else.tar.gz
`);
  const r = await runSh("nolist");
  assert.notEqual(r.status, 0);
  assert.match(r.stderr, /not listed/);
  assert.ok(!existsSync(r.log));
});

test("install.sh: no network gives a clear download message", { skip: !haveSh }, async () => {
  const r = await runSh("good", { HPK_RELEASE_BASE: "http://127.0.0.1:1" });
  assert.notEqual(r.status, 0);
  assert.match(r.stderr, /download/i);
});

test("install.ps1 refuses a bad checksum", { skip: !havePs }, async () => {
  const r = await runPs("bad");
  assert.notEqual(r.status, 0);
  assert.match(r.stderr + r.stdout, /checksum/i);
  assert.ok(!existsSync(join(work, "data-ps", "bin", "hpk.exe")));
});

test("install.ps1: no network gives a clear download message", { skip: !havePs }, async () => {
  const r = await runPs("good", { HPK_RELEASE_BASE: "http://127.0.0.1:1" });
  assert.notEqual(r.status, 0);
  assert.match(r.stderr + r.stdout, /download/i);
});
