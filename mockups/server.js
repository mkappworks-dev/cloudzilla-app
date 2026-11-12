#!/usr/bin/env node
// Zero-dependency static server for the mockups. Run: node mockups/server.js [port]
// Serving needs no Go: generated/ is committed. Regenerate it with `make mockups-generate`.
const http = require("node:http");
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const ROOT = __dirname;
const PORT = Number(process.argv[2] || process.env.PORT || 4000);
const CSS_ROUTE = "/assets/tailwind.css";
const GENERATED_DIR = path.join(ROOT, "generated");
const CACHE_DIR = path.join(ROOT, ".cache");
const CSS_OUT = path.join(CACHE_DIR, "tailwind.css");

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".svg": "image/svg+xml",
  ".json": "application/json",
};

// bin/tailwindcss is gitignored, so a linked worktree has none of its own: fall back to the main checkout's.
function findTailwind() {
  const candidates = [process.env.TAILWIND, path.join(ROOT, "..", "bin", "tailwindcss")];
  try {
    const common = execFileSync("git", ["-C", ROOT, "rev-parse", "--path-format=absolute", "--git-common-dir"], {
      encoding: "utf8",
    }).trim();
    candidates.push(path.join(common, "..", "bin", "tailwindcss"));
  } catch {
    // not in a git checkout; the other candidates still apply
  }
  return candidates.find((p) => p && fs.existsSync(p));
}

function sources() {
  const pages = fs.readdirSync(ROOT).filter((f) => /\.(html|js)$/.test(f) && f !== "server.js");
  const generated = fs.existsSync(GENERATED_DIR) ? fs.readdirSync(GENERATED_DIR).map((f) => path.join("generated", f)) : [];
  return [
    path.join(ROOT, "input.css"),
    path.join(ROOT, "..", "tailwind", "input.css"),
    path.join(ROOT, "..", "cmd", "server", "frontend", "static", "code-themes.css"),
  ].concat(
    pages.concat(generated).map((f) => path.join(ROOT, f)),
  );
}

function buildCSS() {
  const built = fs.existsSync(CSS_OUT) ? fs.statSync(CSS_OUT).mtimeMs : 0;
  if (sources().every((f) => fs.statSync(f).mtimeMs <= built)) return;
  const bin = findTailwind();
  if (!bin) {
    throw new Error("Tailwind CLI not found. Run `make setup-tailwind` or set TAILWIND=/path/to/tailwindcss.");
  }
  fs.mkdirSync(CACHE_DIR, { recursive: true });
  execFileSync(bin, ["-i", path.join(ROOT, "input.css"), "-o", CSS_OUT], { stdio: ["ignore", "ignore", "pipe"] });
}

function resolveFile(urlPath) {
  const rel = decodeURIComponent(urlPath).replace(/^\/+/, "");
  const abs = path.resolve(ROOT, rel || "index.html");
  if (abs !== ROOT && !abs.startsWith(ROOT + path.sep)) return null;
  if (abs.startsWith(CACHE_DIR)) return null;
  // A page moved to generated/ keeps answering at its old address, so links in the hand mocks still work.
  const generated = path.join(GENERATED_DIR, path.basename(abs));
  for (const candidate of [abs, abs + ".html", path.join(abs, "index.html"), generated, generated + ".html"]) {
    if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) return candidate;
  }
  return null;
}

http
  .createServer((req, res) => {
    const { pathname } = new URL(req.url, "http://localhost");
    try {
      let file;
      if (pathname === CSS_ROUTE) {
        buildCSS();
        file = CSS_OUT;
      } else if (pathname === "/favicon.svg") {
        file = path.join(ROOT, "..", "cmd", "server", "frontend", "static", "favicon.svg");
      } else {
        file = resolveFile(pathname);
      }
      if (!file) {
        res.writeHead(404, { "content-type": "text/plain" }).end("Not found");
        return;
      }
      res.writeHead(200, {
        "content-type": TYPES[path.extname(file)] || "application/octet-stream",
        "cache-control": "no-store",
      });
      fs.createReadStream(file).pipe(res);
    } catch (err) {
      res.writeHead(500, { "content-type": "text/plain" }).end(String(err.message || err));
    }
  })
  .listen(PORT, () => console.log(`Mockups: http://localhost:${PORT}`));
