#!/usr/bin/env node

"use strict";

const { execFileSync, execSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const PACKAGE = require("./package.json");
const ext = process.platform === "win32" ? ".exe" : "";
const binaryPath = path.join(__dirname, "bin", `oc${ext}`);

function hasExpectedBinary() {
  if (!fs.existsSync(binaryPath)) return false;
  try {
    const out = execFileSync(binaryPath, ["--version"], { encoding: "utf8", timeout: 5000 });
    return out.includes(PACKAGE.version);
  } catch {
    return false;
  }
}

if (!hasExpectedBinary()) {
  try {
    execSync(`node ${JSON.stringify(path.join(__dirname, "install.js"))}`, {
      cwd: __dirname,
      stdio: "inherit",
    });
  } catch {
    process.exit(1);
  }
}

try {
  execFileSync(binaryPath, process.argv.slice(2), { stdio: "inherit" });
} catch (err) {
  process.exit(err.status || 1);
}
