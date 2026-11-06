import { test, expect } from '@playwright/test';
import fs from 'node:fs';

const fixtures = () => JSON.parse(fs.readFileSync('.cache/fixtures.json', 'utf8'));

test('repository select supports keyboard selection and escape', async ({ page }) => {
  await page.goto('/repos/new');
  const trigger = page.locator('#repo-gitignore-btn');
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  const list = page.getByRole('listbox', { name: 'gitignore' });
  await expect(list).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect(trigger).toBeFocused();
  await expect(page.locator('#repo-gitignore')).not.toHaveValue('');
  await page.keyboard.press('ArrowDown');
  await expect(list).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  await expect(list).toBeHidden();
});

test('milestone tabs respond to arrow and Home keys', async ({ page }) => {
  await page.goto(`${fixtures().milestoneRepo}/milestones`);
  const open = page.getByRole('tab', { name: /^Open/ });
  const closed = page.getByRole('tab', { name: /^Closed/ });
  await open.focus();
  await page.keyboard.press('ArrowRight');
  await expect(closed).toBeFocused();
  await expect(closed).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Home');
  await expect(open).toBeFocused();
  await expect(open).toHaveAttribute('aria-selected', 'true');
});

test('wiki pages can be reordered using keyboard', async ({ page }) => {
  const f = fixtures();
  await page.goto(`${f.ownRepo}/wiki/${f.wiki}`);
  await page.getByRole('button', { name: 'Reorder pages' }).click();
  const items = page.locator('#wiki-page-list li[data-slug]');
  const before = await items.evaluateAll(els => els.map(e => e.dataset.slug));
  const button = page.getByRole('button', { name: `Move ${before[1]} up` });
  await button.focus();
  await page.keyboard.press('Enter');
  await expect.poll(() => items.evaluateAll(els => els.map(e => e.dataset.slug))).toEqual([before[1], before[0], ...before.slice(2)]);
  await expect(button).toBeFocused();
});
