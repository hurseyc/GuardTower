// Regenerates the README screenshots from demo mode:
//   make build && cd web && npm run screenshots
// Uses PW_CHROMIUM_PATH when set (a preinstalled Chromium).
import { spawn } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { chromium } from '@playwright/test'

const port = 18090
const base = `http://127.0.0.1:${port}`
const out = new URL('../../.github/assets/', import.meta.url).pathname
const shots = [
  { name: 'overview', path: '/' },
  { name: 'topology', path: '/topology' },
  { name: 'analytics', path: '/analytics?range=7d' },
  { name: 'family', path: '/family' },
  { name: 'config', path: '/config' },
  { name: 'overview-light', path: '/', theme: 'light' },
]

const server = spawn('../bin/guardtower', ['serve', '--demo', '--listen', `127.0.0.1:${port}`], { stdio: ['ignore', 'ignore', 'inherit'] })
try {
  for (let i = 0; ; i++) {
    try {
      if ((await fetch(`${base}/api/v1/meta`)).ok) break
    } catch {
      /* not up yet */
    }
    if (i > 120) throw new Error('GuardTower did not start')
    await new Promise((r) => setTimeout(r, 500))
  }
  await new Promise((r) => setTimeout(r, 12_000)) // let the collector pick up live traffic
  mkdirSync(out, { recursive: true })
  const browser = await chromium.launch(process.env.PW_CHROMIUM_PATH ? { executablePath: process.env.PW_CHROMIUM_PATH } : {})
  for (const theme of ['dark', 'light']) {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: theme })
    await ctx.addInitScript((t) => {
      localStorage.setItem('gt-theme', t)
      localStorage.setItem('gt.getting-started.dismissed', '1')
    }, theme)
    const page = await ctx.newPage()
    await page.goto(base)
    await page.getByRole('button', { name: 'Fill in demo account' }).click()
    await page.getByRole('button', { name: 'Sign in' }).click()
    await page.getByRole('heading', { name: 'Overview' }).waitFor()
    for (const s of shots.filter((s) => (s.theme ?? 'dark') === theme)) {
      await page.goto(base + s.path)
      await page.waitForLoadState('networkidle')
      await page.waitForTimeout(1500) // charts animate in
      await page.screenshot({ path: `${out}${s.name}.png` })
      console.log('wrote', `${out}${s.name}.png`)
    }
    await ctx.close()
  }
  await browser.close()
} finally {
  server.kill()
}
