import { chromium } from '@playwright/test';
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import globalSetup from './global-setup.js';
import { baseURL } from './playwright.config.js';
import { routes, resolve } from './routes.js';

const output = path.resolve(process.env.CZ_A11Y_SCREENSHOTS || '.cache/screenshots');
const mockURL = 'http://127.0.0.1:18633';
const server = spawn(process.execPath, ['../../mockups/server.js', '18633'], { stdio: 'inherit' });
let browser;
try {
  let ready = false;
  for (let i = 0; i < 50; i++) {
    try { ready = (await fetch(`${mockURL}/assets/tailwind.css`)).ok; } catch {}
    if (ready) break;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  if (!ready) throw new Error('Mockups server did not start or Tailwind CSS did not build');
  await globalSetup();
  const fixtures = JSON.parse(fs.readFileSync('.cache/fixtures.json', 'utf8'));
  const mocks = fs.readdirSync('../../mockups').filter(name => name.endsWith('.html') && name !== 'index.html');
  browser = await chromium.launch();
  for (const theme of ['light', 'dark']) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: theme, storageState: '.cache/admin.json' });
    await context.addInitScript(theme => localStorage.setItem('cz-theme', theme), theme);
    const page = await context.newPage();
    const anonymous = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: theme });
    await anonymous.addInitScript(theme => localStorage.setItem('cz-theme', theme), theme);
    const anonPage = await anonymous.newPage();
    fs.mkdirSync(path.join(output, theme), { recursive: true });
    for (const route of routes) {
      const target = route.anon ? anonPage : page;
      const url = new URL(resolve(route.path, fixtures), baseURL);
      const response = await target.goto(url.href);
      if (response.status() !== (route.status ?? 200) || new URL(target.url()).pathname !== url.pathname) {
        throw new Error(`${route.name}: unexpected response or redirect`);
      }
      await target.evaluate(() => document.fonts.ready);
      await target.screenshot({ path: path.join(output, theme, `real-${route.name.replaceAll(' ', '-')}.png`), fullPage: true });
      if (route.name === 'wiki') {
        await target.getByRole('button', { name: 'Reorder pages' }).click();
        await target.screenshot({ path: path.join(output, theme, 'real-wiki-reorder.png'), fullPage: true });
      }
    }
    for (const name of mocks) {
      const response = await page.goto(`${mockURL}/${name}`);
      if (!response.ok()) throw new Error(`${name}: ${response.status()}`);
      await page.evaluate(() => document.fonts.ready);
      await page.screenshot({ path: path.join(output, theme, `mock-${name.replace('.html', '.png')}`), fullPage: true });
      if (name === 'wiki.html') {
        await page.getByRole('button', { name: 'Reorder pages' }).click();
        await page.screenshot({ path: path.join(output, theme, 'mock-wiki-reorder.png'), fullPage: true });
      }
    }
    await context.close();
    await anonymous.close();
  }
  console.log(`Desktop screenshots: ${output}`);
} finally {
  await browser?.close();
  server.kill();
}
