const fs = require("node:fs");
const path = require("node:path");

const packagePath = path.join(__dirname, "sdk", "nodejs", "package.json");
const pkg = JSON.parse(fs.readFileSync(packagePath, "utf8"));
pkg.name = "@use-lock/pulumi";
pkg.main = "index.js";
pkg.types = "index.d.ts";
pkg.type = "commonjs";
pkg.engines = { node: ">=22" };
pkg.repository = { type: "git", url: "git+https://github.com/use-lock/pulumi.git" };
pkg.publishConfig = { access: "public" };
fs.writeFileSync(packagePath, JSON.stringify(pkg, null, 2) + "\n");
