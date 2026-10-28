import { AxeBuilder } from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

// Runs after fleet.spec.ts (see playwright.config.ts), so every page has data.
const ADMIN = { user: 'admin', pass: 'e2e-admin-password' }

async function signIn(page: Page) {
  await page.goto('/')
  await page.getByLabel('Username').fill(ADMIN.user)
  await page.getByLabel('Password', { exact: true }).fill(ADMIN.pass)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
}

// Every page in src/lib/routes.ts, plus a few sub-pages.
const pages = ['/', '/instances', '/instances/1', '/groups', '/topology', '/hosts', '/config', '/upgrades', '/changes', '/analytics', '/logs',
  '/family', '/family/devices', '/family/settings', '/users', '/audit', '/settings', '/docs', '/docs/getting-started/demo']

for (const theme of ['light', 'dark'] as const) {
  test.describe(`accessibility (${theme})`, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript((t) => localStorage.setItem('gt-theme', t), theme)
    })

    test('sign-in page', async ({ page }) => {
      await page.goto('/')
      await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
      await check(page, 'sign-in')
    })

    test('every page', async ({ page }) => {
      test.setTimeout(120_000)
      await signIn(page)
      const failures: string[] = []
      for (const path of pages) {
        await page.goto(path)
        await page.waitForLoadState('networkidle')
        await page.waitForTimeout(400) // charts and lazy panels settle
        failures.push(...(await violations(page, path)))
      }
      expect(failures, failures.join('\n\n')).toEqual([])
    })
  })
}

async function violations(page: Page, where: string) {
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  return r.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${where}: [${v.impact}] ${v.id}: ${v.help}\n  ${v.nodes.slice(0, 4).map((n) => n.target.join(' ') + ' — ' + (n.failureSummary ?? '').split('\n')[1]?.trim()).join('\n  ')}`)
}

async function check(page: Page, where: string) {
  const f = await violations(page, where)
  expect(f, f.join('\n\n')).toEqual([])
}
