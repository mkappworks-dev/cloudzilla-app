// Signs in as the seeded superadmin, adds the content the seed leaves out (wiki
// pages, an own gist), and resolves every {placeholder} in routes.js to a real
// path on this server. Discovery instead of hard-coded names keeps the gate
// working when the seed's output changes.
import { chromium } from '@playwright/test';
import fs from 'node:fs';
import { baseURL } from './playwright.config.js';

export const ADMIN = { email: 'admin@example.test', username: 'siteadmin', password: 'cloudzilla-seed' };
export const WIKI_PAGES = ['Home', 'Architecture', 'Contributing'];

export default async function globalSetup() {
  fs.mkdirSync('.cache', { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({ baseURL });

  await page.goto('/login');
  await page.getByLabel('Email').fill(ADMIN.email);
  await page.getByLabel('Password', { exact: true }).fill(ADMIN.password);
  await Promise.all([page.waitForURL(u => !u.pathname.startsWith('/login')), page.getByRole('button', { name: 'Sign in' }).click()]);
  await page.context().storageState({ path: '.cache/admin.json' });

  const csrf = (await page.context().cookies()).find(c => c.name === 'csrf_token')?.value ?? '';
  const api = (method, path, opts = {}) =>
    page.request.fetch(path, { method, headers: { 'X-CSRF-Token': csrf, ...opts.headers }, ...opts }).then(async res => {
      if (!res.ok() && res.status() !== 303) throw new Error(`${method} ${path}: ${res.status()} ${await res.text()}`);
      return res;
    });

  async function links(path, pattern) {
    const res = await page.goto(path);
    if (!res.ok()) return [];
    const hrefs = await page.locator('main a[href]').evaluateAll(as => as.map(a => a.getAttribute('href')));
    return hrefs.filter(h => pattern.test(h.split(/[?#]/)[0])).map(h => h.split('#')[0]);
  }
  async function first(paths, pattern) {
    for (const p of paths) {
      const [hit] = await links(p, pattern);
      if (hit) return hit;
    }
    throw new Error(`no link matching ${pattern} on ${paths.join(', ')}`);
  }

  let repos = await (await api('GET', '/api/repos/')).json();
  const ownRepo = `/${ADMIN.username}/a11y-fixture`;
  if (!repos.some(r => r.owner_name === ADMIN.username && r.name === 'a11y-fixture')) {
    await api('POST', '/api/repos/', { data: { name: 'a11y-fixture', add_readme: true, private: false } });
    repos = await (await api('GET', '/api/repos/')).json();
  }
  const orgCheck = await page.request.get('/api/orgs/a11y-fixture-org');
  if (orgCheck.status() === 404) {
    await api('POST', '/api/orgs/', { data: { name: 'a11y-fixture-org', display_name: 'A11y fixture' } });
  } else if (!orgCheck.ok()) throw new Error(`Cannot inspect fixture organization: ${orgCheck.status()}`);

  for (const slug of WIKI_PAGES) {
    await api('POST', `/api/repos${ownRepo}/wiki/${slug}`, {
      form: { content: `# ${slug}\n\nSee [the README](../) and [${WIKI_PAGES[0]}](${WIKI_PAGES[0]}).`, message: `Add ${slug}` },
      maxRedirects: 0,
    });
  }
  await page.goto('/gists');
  const existingGist = page.locator('li[data-name="a11y fixture siteadmin"] > a').first();
  let gistPath = await existingGist.count() ? await existingGist.getAttribute('href') : null;
  if (!gistPath) {
    const gist = await (await api('POST', '/api/gists', {
      data: { description: 'a11y fixture', public: true, files: [{ filename: 'main.go', content: 'package main\n\nfunc main() {}\n' }] },
    })).json();
    gistPath = `/gists/${gist.id}`;
  }

  // The busiest public repo carries the issue, PR and discussion pages.
  const publicRepos = repos.filter(r => !r.private && !r.is_archived && r.name !== 'a11y-fixture').map(r => `/${r.owner_name}/${r.name}`);
  const counts = await Promise.all(
    publicRepos.map(async p => (await (await api('GET', `/api/repos${p}/issues/`)).json()).length),
  );
  const busy = publicRepos.map((p, i) => [p, counts[i]]).sort((a, b) => b[1] - a[1]).map(([p]) => p);
  const repo = busy[0];
  const each = suffix => busy.map(p => p + suffix);
  const num = s => new RegExp(`^/[^/]+/[^/]+/${s}/\\d+$`);

  const fixtures = {
    me: ADMIN.username,
    repo,
    ownRepo,
    wiki: WIKI_PAGES[0],
    gist: gistPath,
    blob: await first([repo], /^\/[^/]+\/[^/]+\/blob\/.+/),
    commit: await first([`${repo}/commits`], /^\/[^/]+\/[^/]+\/commit\/[0-9a-f]{7,}$/),
    issue: await first([`${repo}/issues`], num('issues')),
    closedIssue: await first(each('/issues?state=closed'), num('issues')),
    milestone: await first(each('/milestones'), num('milestones')),
    pull: await first(each('/pulls'), num('pulls')),
    discussion: await first(each('/discussions'), num('discussions')),
    project: await first(each('/projects'), num('projects')),
    release: await first(each('/releases'), /^\/[^/]+\/[^/]+\/releases\/tag\/.+$/),
    org: 'a11y-fixture-org',
    user: repos.find(r => !r.org_id && r.owner_name !== ADMIN.username).owner_name,
  };
  const repoOf = path => path.split('/').slice(0, 3).join('/');
  Object.assign(fixtures, {
    blame: fixtures.blob.replace('/blob/', '/blame/'),
    milestoneRepo: repoOf(fixtures.milestone),
    discussionRepo: repoOf(fixtures.discussion),
    projectRepo: repoOf(fixtures.project),
    releaseRepo: repoOf(fixtures.release),
  });
  fs.writeFileSync('.cache/fixtures.json', JSON.stringify(fixtures, null, 2));
  await browser.close();
}
