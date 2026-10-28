import { expect, test, type Page } from '@playwright/test'

const ADMIN = { user: 'admin', pass: 'e2e-admin-password' }

async function signIn(page: Page) {
  await page.goto('/')
  await page.getByLabel('Username').fill(ADMIN.user)
  await page.getByLabel('Password', { exact: true }).fill(ADMIN.pass)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
}

test.describe.serial('first run and fleet setup', () => {
  test('first-run wizard creates the admin', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Welcome to GuardTower' })).toBeVisible()
    await page.getByLabel('Admin username').fill(ADMIN.user)
    await page.getByLabel('Password', { exact: true }).fill(ADMIN.pass)
    await page.getByLabel('Confirm password').fill(ADMIN.pass)
    await page.getByRole('button', { name: /Create admin/ }).click()
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
    await expect(page.getByText('Add your first AdGuard Home instance')).toBeVisible()
  })

  test('add a group and two instances', async ({ page }) => {
    await signIn(page)
    await page.getByRole('link', { name: 'Groups' }).click()
    await page.getByRole('button', { name: 'Create a group' }).click()
    await page.getByLabel('Name', { exact: true }).fill('Main network')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByRole('heading', { name: 'Main network' })).toBeVisible()

    await page.getByRole('link', { name: 'Instances' }).click()
    for (const n of [1, 2]) {
      await page.getByRole('button', { name: 'Add instance' }).first().click()
      const dlg = page.getByRole('dialog')
      await dlg.getByLabel('Name', { exact: true }).fill(`agh-${n}`)
      await dlg.getByLabel('URL').fill(`http://127.0.0.1:1300${n}`)
      await dlg.getByLabel('Username').fill('admin')
      await dlg.getByLabel('Password', { exact: true }).fill('devfleet-password')
      await dlg.getByLabel('Settings group').selectOption({ label: 'Main network' })
      await dlg.getByRole('button', { name: 'Test connection' }).click()
      await expect(dlg.getByText(/Connected to AdGuard Home v0\.107/)).toBeVisible()
      await dlg.getByRole('button', { name: 'Add instance' }).click()
      await expect(page.getByRole('link', { name: `agh-${n}` })).toBeVisible()
    }
  })

  test('overview shows the instances online', async ({ page }) => {
    await signIn(page)
    await expect(page.getByText('2/2')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('All instances online')).toBeVisible()
  })

  test('contextual help and docs search work', async ({ page }) => {
    await signIn(page)
    await page.getByRole('button', { name: 'Help' }).click()
    await expect(page.getByRole('dialog').getByRole('heading', { name: 'The overview dashboard' })).toBeVisible()
    await page.keyboard.press('Escape')
    await page.goto('/docs')
    await page.getByPlaceholder('Search docs…').first().fill('backup')
    await page.getByRole('link', { name: /Backup & restore/ }).first().click()
    await expect(page.getByRole('heading', { name: 'Backup & restore' })).toBeVisible()
  })

  test('wrong password is rejected', async ({ page }) => {
    await page.goto('/')
    await page.getByLabel('Username').fill(ADMIN.user)
    await page.getByLabel('Password', { exact: true }).fill('not-the-password')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByText('invalid username or password')).toBeVisible()
  })
})

test.describe.serial('query log and analytics', () => {
  test('queries sent to an instance appear in the query log and analytics', async ({ page }) => {
    // Generate a few real DNS queries against agh-1 via the dev tool.
    const { execSync } = await import('node:child_process')
    execSync('go run ./cmd/gtdev traffic -n 2 -duration 4s -qps 20', { cwd: '..', stdio: 'ignore' })
    await signIn(page)
    await page.getByRole('link', { name: 'Query log' }).click()
    await expect(page.getByRole('heading', { name: 'Query log' })).toBeVisible()
    // Collection runs every 10 s; rows appear live.
    await expect(page.locator('tbody tr').first()).toBeVisible({ timeout: 30_000 })
    await page.getByLabel('Domain').fill('google.com')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(/domain=google\.com/)
    await page.getByRole('link', { name: 'Analytics' }).click()
    await expect(page.getByText('DNS queries')).toBeVisible()
    await expect(page.getByText('Traffic by instance')).toBeVisible()
  })
})

test.describe.serial('review & apply', () => {
  test('observe mode guards apply and the plan previews', async ({ page }) => {
    await signIn(page)
    await page.getByRole('link', { name: 'Review & apply' }).click()
    await expect(page.getByRole('heading', { name: 'Review & apply' })).toBeVisible()
    await expect(page.getByText('Observe mode is on.')).toBeVisible()
    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/^2\. Review/)).toBeVisible()
    await page.getByRole('button', { name: 'Allow applying changes' }).click()
    await page.getByRole('button', { name: 'Turn off observe mode' }).click()
    await expect(page.getByText('Observe mode is on.')).toBeHidden()
    // Settings reflects the same switch; turn observe mode back on there.
    await page.getByRole('link', { name: 'Settings' }).click()
    const sw = page.getByRole('switch', { name: /Off: changes can be applied/ })
    await expect(sw).toHaveAttribute('aria-checked', 'false')
    await sw.click()
    await expect(page.getByRole('switch', { name: /On: read-only/ })).toHaveAttribute('aria-checked', 'true')
  })
})
