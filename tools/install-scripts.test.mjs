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
  // Windows installs through Docker: install.ps1 fetches the compose file.
  fs.copyFileSync(join(root, "docker", "compose.release.yaml"), join(work, "rel", "good", "compose.release.yaml"));
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
    HPK_DIR: join(work, `dir-ps-${sub}`), HPK_PROJECTS: work, ...extra,
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

test("install.ps1 sets up the compose file and settings", { skip: !havePs }, async () => {
  const r = await runPs("good");
  assert.equal(r.status, 0, r.stdout + r.stderr);
  const dir = join(work, "dir-ps-good");
  assert.deepEqual(readFileSync(join(dir, "compose.release.yaml")), readFileSync(join(root, "docker", "compose.release.yaml")));
  const env = readFileSync(join(dir, ".env"), "utf8");
  const lines = env.split(/\r?\n/);
  assert.ok(!env.includes("HPK_OWNER"), "solo mode needs no owner");
  assert.ok(lines.includes("HPK_PROJECTS=" + work.replaceAll("\\", "/")), env);
  assert.notEqual(env.charCodeAt(0), 0xfeff, ".env must have no BOM");
});

test("install.ps1 refuses a bad checksum", { skip: !havePs }, async () => {
  const r = await runPs("bad");
  assert.notEqual(r.status, 0);
  assert.match(r.stderr + r.stdout, /checksum/i);
  assert.ok(!existsSync(join(work, "dir-ps-bad", "compose.release.yaml")));
});

test("install.ps1: no network gives a clear download message", { skip: !havePs }, async () => {
  const r = await runPs("good", { HPK_RELEASE_BASE: "http://127.0.0.1:1" });
  assert.notEqual(r.status, 0);
  assert.match(r.stderr + r.stdout, /download/i);
});

// The English and Thai guides must have the same Quick install headings.
test("install.md and install.th.md: same Quick install heading structure", () => {
  const quick = (file) => {
    const text = readFileSync(join(root, "docs", file), "utf8").replace(/```[\s\S]*?```/g, "");
    const lines = text.split(/\r?\n/);
    const start = lines.findIndex((l) => /^## /.test(l) && !/^## ?$/.test(l) && !/^# /.test(l));
    assert.ok(start >= 0, `${file}: no ## heading`);
    let end = lines.findIndex((l, i) => i > start && /^## /.test(l));
    if (end < 0) end = lines.length;
    return lines.slice(start, end).filter((l) => /^#{2,3} /.test(l));
  };
  const en = quick("install.md");
  const th = quick("install.th.md");
  assert.equal(en.filter((l) => l.startsWith("## ")).length, 1);
  assert.equal(en.filter((l) => l.startsWith("### ")).length, 8);
  assert.deepEqual(
    th.map((l) => l.match(/^#+/)[0]),
    en.map((l) => l.match(/^#+/)[0]),
  );
  // Numbered steps appear in the same order.
  const nums = (a) => a.map((l) => (l.match(/^### (\d+)\./) || [])[1]);
  assert.deepEqual(nums(th), nums(en));
});

// The fenced code blocks of the Quick install parts must be identical lists.
test("install.md and install.th.md: same Quick install code blocks", () => {
  const blocks = (file) => {
    const lines = readFileSync(join(root, "docs", file), "utf8").split(/\r?\n/);
    const start = lines.findIndex((l) => /^## /.test(l));
    let end = lines.findIndex((l, i) => i > start && /^## /.test(l));
    if (end < 0) end = lines.length;
    const part = lines.slice(start, end).join("\n");
    return [...part.matchAll(/```[a-z]*\n([\s\S]*?)```/g)].map((m) => m[1].trim());
  };
  const en = blocks("install.md");
  const th = blocks("install.th.md");
  assert.ok(en.length >= 8, `expected several code blocks, got ${en.length}`);
  assert.deepEqual(th, en);
});

// Without a terminal (stdout piped, as here) install.sh must not run hpk in the
// foreground: it starts it in the background with --no-open and a log file,
// and tells the person the address.
test("install.sh without a terminal starts hpk in the background with --no-open", { skip: !haveSh }, async () => {
  const bin = join(work, "data", "bin");
  mkdirSync(bin, { recursive: true });
  const stub = join(bin, "hpk");
  writeFileSync(stub, '#!/bin/sh\necho "start $@" >> "$STUB_LOG"\n');
  spawnSync("sh", ["-c", 'chmod +x "$1"', "sh", stub.replaceAll("\\", "/")]);
  const r = await runSh("good", { HPK_NO_START: "" });
  assert.equal(r.status, 0, r.stdout + r.stderr);
  assert.match(r.stdout, /http:\/\/127\.0\.0\.1:7878/);
  assert.match(r.stdout, /hpk\.log/);
  let text = "";
  for (let i = 0; i < 40 && !/start --no-open/.test(text); i++) {
    await new Promise((res) => setTimeout(res, 100));
    text = existsSync(r.log) ? readFileSync(r.log, "utf8") : "";
  }
  assert.match(text, /start --no-open/);
});

test("install.sh wraps its body in main() and calls it last", () => {
  const lines = readFileSync(join(root, "install.sh"), "utf8").trimEnd().split("\n");
  assert.equal(lines[lines.length - 1], 'main "$@"');
  assert.ok(lines.some((l) => l === "main() {"));
});

// The image name in the release compose file must use the repository owner
// (lower-cased, as GHCR requires), not a typo of it.
test("compose.release.yaml image owner matches the git remote owner", () => {
  const r = spawnSync("git", ["remote", "get-url", "origin"], { cwd: root, encoding: "utf8" });
  if (r.error || r.status !== 0) return; // no remote: nothing to compare
  const m = r.stdout.trim().match(/github\.com[:/]([^/]+)\//i);
  if (!m) return;
  const compose = readFileSync(join(root, "docker", "compose.release.yaml"), "utf8");
  const img = compose.match(/image:\s*ghcr\.io\/([^/\s]+)\/hpk-hermes:/);
  assert.ok(img, "no ghcr image line in compose.release.yaml");
  assert.equal(img[1], m[1].toLowerCase());
  for (const f of ["docs/docker.md", "CHANGELOG.md", ".github/workflows/release.yml"]) {
    const t = readFileSync(join(root, f), "utf8");
    for (const [, owner] of t.matchAll(/ghcr\.io\/([a-z0-9-]+)\/hpk-hermes/gi)) {
      assert.equal(owner, m[1].toLowerCase(), `${f} names the wrong image owner`);
    }
  }
});
