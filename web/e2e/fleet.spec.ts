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

test.describe.serial('upgrades', () => {
  test('upgrades page lists instances and saves a group policy', async ({ page }) => {
    await signIn(page)
    await page.getByRole('link', { name: 'Upgrades' }).click()
    await expect(page.getByRole('heading', { name: 'AdGuard Home upgrades' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'agh-1' })).toBeVisible()
    await page.getByLabel('Upgrade policy for Main network').selectOption('auto')
    await page.getByRole('button', { name: 'Sun' }).first().click()
    await page.getByRole('row', { name: /Main network/ }).getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('Upgrade policy saved for Main network')).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Upgrade policy for Main network')).toHaveValue('auto')
  })

  test('instance page configures the Docker upgrade method', async ({ page }) => {
    await signIn(page)
    await page.getByRole('link', { name: 'Instances' }).click()
    await page.getByRole('link', { name: 'agh-2' }).click()
    await page.getByLabel('Upgrade method').selectOption('docker')
    await page.getByLabel('Docker API endpoint').fill('tcp://127.0.0.1:1')
    await page.getByLabel('Container name').fill('adguardhome')
    await page.getByRole('button', { name: 'Test Docker connection' }).click()
    await expect(page.getByText(/cannot reach the Docker API/)).toBeVisible()
    await page.getByRole('button', { name: 'Save upgrade settings' }).click()
    await expect(page.getByText('Upgrade settings saved')).toBeVisible()
  })
})

test.describe.serial('resilience and topology', () => {
  test('placing both instances on one host warns before saving and flags the group', async ({ page }) => {
    await signIn(page)
    await page.getByRole('link', { name: 'Hosts & sites' }).click()
    await page.getByRole('button', { name: 'New host' }).first().click()
    const hd = page.getByRole('dialog')
    await hd.getByLabel('Name').fill('nas-1')
    await hd.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('nas-1', { exact: true })).toBeVisible()

    await page.getByRole('link', { name: 'Instances' }).click()
    for (const n of [1, 2]) {
      await page.getByRole('row', { name: new RegExp(`agh-${n}`) }).getByRole('button', { name: 'Edit' }).click()
      const dlg = page.getByRole('dialog')
      await dlg.getByLabel('Runs on host').selectOption({ label: 'nas-1' })
      if (n === 2) await expect(dlg.getByText(/All 2 DNS servers of Main network run on nas-1/)).toBeVisible()
      await dlg.getByRole('button', { name: /^Save/ }).click()
      await expect(dlg).toBeHidden()
    }

    await page.getByRole('link', { name: 'Groups' }).click()
    await expect(page.getByText(/^Single point of failure · 1 host/)).toBeVisible()
    await page.getByRole('link', { name: 'Overview' }).click()
    await expect(page.getByRole('heading', { name: 'Single points of failure' })).toBeVisible()
  })

  test('topology draws hosts and instances and focuses a group', async ({ page }) => {
    await signIn(page)
    await page.getByRole('link', { name: 'Topology', exact: true }).click()
    await expect(page.getByText('only host of Main network')).toBeVisible()
    await expect(page.locator('.react-flow__node').filter({ hasText: 'agh-1' })).toBeVisible()
    await page.getByRole('button', { name: /Main network/ }).click()
    await expect(page).toHaveURL(/group=\d+/)
    await expect(page.getByText(/All 2 DNS servers of Main network run on nas-1/)).toBeVisible()
    await page.getByRole('button', { name: /By group/ }).click()
    await expect(page).toHaveURL(/view=logical/)
    await page.locator('.react-flow__node').filter({ hasText: 'agh-2' }).click()
    await expect(page).toHaveURL(/\/instances\/\d+/)
  })
})

test.describe.serial('family dns', () => {
  test('a person gets their devices attempts, masked with reveal', async ({ page }) => {
    test.setTimeout(120_000)
    await signIn(page)
    await page.getByRole('link', { name: 'Family DNS' }).click()
    await expect(page.getByRole('heading', { name: 'Family DNS' })).toBeVisible()
    await page.getByRole('link', { name: 'People & devices', exact: true }).click()
    await page.getByRole('button', { name: 'Add person' }).click()
    await page.getByRole('dialog').getByLabel('Name').fill('Sam')
    await page.getByRole('dialog').getByRole('button', { name: 'Save' }).click()
    await expect(page.getByText('Sam added')).toBeVisible()
    // The traffic test earlier sent adult-site lookups from 127.0.0.1; sessions
    // are built every 30 s.
    await expect(async () => {
      await page.reload()
      await expect(page.getByLabel(/Owner of /).first()).toBeVisible({ timeout: 2000 })
    }).toPass({ timeout: 60_000 })
    await page.getByLabel(/Owner of /).first().selectOption({ label: 'Sam' })
    await expect(page.getByText('Device updated')).toBeVisible()
    await page.getByRole('navigation', { name: 'Family DNS sections' }).getByRole('link', { name: 'Overview' }).click()
    await expect(async () => {
      await page.reload()
      await expect(page.getByRole('link', { name: /Sam/ }).getByText(/^[1-9]\d*$/).first()).toBeVisible({ timeout: 2000 })
    }).toPass({ timeout: 60_000 })
    await page.getByRole('link', { name: /Sam/ }).first().click()
    const masked = page.getByText(/^[a-z]\*\*\*/).first()
    await expect(masked).toBeVisible()
    await page.getByRole('button', { name: /Reveal domain/ }).first().click()
    await expect(page.getByRole('button', { name: 'Hide domain' })).toBeVisible()
  })
})

test.describe.serial('GuardTower updates', () => {
  // The test server is a dev build without the update helper, so the
  // update API is faked here to walk through the whole flow in the UI.
  test('the header button lights up and installs a new release', async ({ page }) => {
    let phase: 'available' | 'updating' | 'done' = 'available'
    const status = () => ({
      current: phase === 'done' ? 'v9.9.9' : 'v1.0.0',
      latest: 'v9.9.9',
      url: 'https://github.com/hurseyc/GuardTower/releases/tag/v9.9.9',
      update_available: phase !== 'done',
      method: 'systemd',
      can_self_update: true,
      updating: phase === 'updating',
    })
    await page.route('**/api/v1/updates/guardtower', (r) => r.fulfill({ json: status() }))
    await page.route('**/api/v1/updates/guardtower/install', (r) => {
      phase = 'updating'
      setTimeout(() => (phase = 'done'), 4000)
      return r.fulfill({ status: 202, json: status() })
    })
    await signIn(page)
    const button = page.getByRole('button', { name: 'Update', exact: true })
    await expect(button).toBeVisible()
    await button.click()
    await expect(page.getByRole('dialog')).toContainText('v9.9.9 is available')
    await page.getByRole('button', { name: 'Update to v9.9.9' }).click()
    await expect(page.getByRole('dialog')).toContainText('Downloading and verifying the release')
    await expect(page.getByText('GuardTower v9.9.9 is installed')).toBeVisible({ timeout: 15_000 })
    await page.waitForEvent('load')
    await expect(page.getByRole('button', { name: 'v9.9.9' })).toBeVisible()
  })
})
