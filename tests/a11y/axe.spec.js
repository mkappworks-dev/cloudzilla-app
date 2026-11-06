import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import fs from 'node:fs';
import { routes, resolve } from './routes.js';
import { unallowed } from './gate.js';

const fixtures = () => JSON.parse(fs.readFileSync('.cache/fixtures.json', 'utf8'));

for (const route of routes) {
  test.describe(() => {
    if (route.anon) test.use({ storageState: { cookies: [], origins: [] } });

    test(route.name, async ({ page }, info) => {
      const theme = info.project.use.colorScheme;
      await page.addInitScript(t => localStorage.setItem('cz-theme', t), theme);
      const url = resolve(route.path, fixtures());
      const res = await page.goto(url);
      expect(res.status(), `${url} status`).toBe(route.status ?? 200);
      const expectedURL = new URL(url, page.url());
      expect(new URL(page.url()).pathname, `${url} final path`).toBe(expectedURL.pathname);
      expect(new URL(page.url()).search, `${url} final query`).toBe(expectedURL.search);
      await expect(page.locator('main')).toBeVisible();
      await page.waitForLoadState('networkidle');

      const { violations } = await new AxeBuilder({ page }).analyze();
      const failures = unallowed(route.name, theme, violations);
      expect(failures, `${failures.length} serious/critical axe violations on ${url} (${theme}):\n  ${JSON.stringify(failures, null, 2)}\n`).toEqual([]);
    });
  });
}


test('gate rejects an injected serious violation', async ({ page }, info) => {
  await page.goto('/');
  await page.locator('main').evaluate(main => {
    const image = document.createElement('img');
    image.id = 'a11y-gate-injected-image';
    image.src = '/static/favicon.svg';
    main.appendChild(image);
  });
  const { violations } = await new AxeBuilder({ page }).analyze();
  expect(unallowed('home', info.project.use.colorScheme, violations)).toEqual(
    expect.arrayContaining([expect.objectContaining({ rule: 'image-alt', target: '#a11y-gate-injected-image' })]),
  );
});
