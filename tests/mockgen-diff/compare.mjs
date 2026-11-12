// Compares generated mocks with the live server's render of the same page: tags, classes and attribute names, never
// text, so seeded data and fixtures may differ. Run against a throwaway seeded server (see docs/testing.md):
//
//   CZ_BASE_URL=http://127.0.0.1:18641 CZ_EMAIL=admin@example.test CZ_PASSWORD=cloudzilla-seed node compare.mjs
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const generated = join(here, "..", "..", "mockups", "generated");
const base = process.env.CZ_BASE_URL;
if (!base) throw new Error("set CZ_BASE_URL to a seeded throwaway server");
const email = process.env.CZ_EMAIL ?? "admin@example.test";
const password = process.env.CZ_PASSWORD ?? "cloudzilla-seed";

// Only variants a plain GET reaches. A variant that needs a failed form post or a toggled setting has no live URL.
// "email" names another seeded account (the seed gives the default admin no repositories on the home page).
const pages = JSON.parse(readFileSync(join(here, "routes.json"), "utf8"));

class Jar {
  cookies = new Map();
  absorb(res) {
    for (const c of res.headers.getSetCookie()) {
      const [pair] = c.split(";");
      const i = pair.indexOf("=");
      this.cookies.set(pair.slice(0, i), pair.slice(i + 1));
    }
  }
  header() {
    return [...this.cookies].map(([k, v]) => `${k}=${v}`).join("; ");
  }
}

async function get(jar, path) {
  const res = await fetch(base + path, { headers: { cookie: jar.header() }, redirect: "manual" });
  jar.absorb(res);
  return res;
}

const sessions = new Map();

// Signs in once per account; a page's route may name another seeded account when the default one has no data for it.
async function signIn(email) {
  if (sessions.has(email)) return sessions.get(email);
  const jar = new Jar();
  await get(jar, "/login");
  const body = new URLSearchParams({ email, password, csrf_token: jar.cookies.get("csrf_token") ?? "" });
  const res = await fetch(base + "/login", {
    method: "POST",
    headers: { cookie: jar.header(), "content-type": "application/x-www-form-urlencoded" },
    body,
    redirect: "manual",
  });
  jar.absorb(res);
  const to = res.headers.get("location") ?? "";
  if (res.status < 300 || res.status >= 400 || to.startsWith("/login")) throw new Error(`sign-in failed for ${email}: ${res.status} ${to}`);
  sessions.set(email, jar);
  return jar;
}

// Drops script bodies and the framework attributes the mocks deliberately omit, then keeps tag, sorted classes and
// attribute names for every element in <body>.
function signatures(html) {
  const at = html.indexOf("<body");
  if (at < 0) throw new Error("response has no <body>");
  const body = html.slice(at);
  const stripped = body.replace(/<script\b[\s\S]*?<\/script>/g, "").replace(/<!--[\s\S]*?-->/g, "");
  const out = new Set();
  for (const m of stripped.matchAll(/<([a-zA-Z][\w-]*)((?:\s+[^\s=>/]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+))?)*)\s*\/?>/g)) {
    const [, tag, attrs] = m;
    const names = [];
    let classes = "";
    for (const a of attrs.matchAll(/([^\s=>/]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g)) {
      const name = a[1];
      if (/^(x-|hx-|@|:)/.test(name)) continue;
      if (name === "class") classes = (a[2] ?? a[3] ?? a[4] ?? "").split(/\s+/).filter(Boolean).sort().join(" ");
      else names.push(name);
    }
    out.add(`${tag}.${classes} [${names.sort().join(",")}]`);
  }
  return out;
}

let failed = 0;
for (const p of pages) {
  const jar = p.auth ? await signIn(p.email ?? email) : new Jar();
  let route = p.route;
  if (p.resolve) {
    // A URL that holds seeded data, such as a commit SHA, is read off a page that links to it.
    const m = (await (await get(jar, p.resolve.from)).text()).match(new RegExp(p.resolve.match));
    if (!m) throw new Error(`${p.file}: ${p.resolve.match} not found on ${p.resolve.from}`);
    route = m[1];
  }
  const res = await get(jar, route);
  const live = signatures(await res.text());
  const mock = signatures(readFileSync(join(generated, `${p.file}.html`), "utf8"));
  // "allow" holds regexes for signatures that vary with seeded data (a language dot colour, a heatmap level), each
  // with the reason it is expected.
  const allowed = (p.allow ?? []).map((a) => new RegExp(a.match));
  const varies = (s) => allowed.some((re) => re.test(s));
  const onlyLive = [...live].filter((s) => !mock.has(s) && !varies(s));
  const onlyMock = [...mock].filter((s) => !live.has(s) && !varies(s));
  const ok = onlyLive.length === 0 && onlyMock.length === 0;
  console.log(`${ok ? "ok  " : "DIFF"} ${p.file}  ${route} (${res.status})`);
  for (const s of onlyLive) console.log(`   live only: ${s}`);
  for (const s of onlyMock) console.log(`   mock only: ${s}`);
  if (!ok) failed++;
}
process.exit(failed ? 1 : 0);
