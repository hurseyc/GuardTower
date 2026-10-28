import { describe, expect, it } from 'vitest'
import { docForPath, navRoutes } from './routes'

describe('docForPath', () => {
  it('maps exact and parameterised routes', () => {
    expect(docForPath('/')).toBe('operation/dashboard')
    expect(docForPath('/instances')).toBe('setup/adding-instances')
    expect(docForPath('/instances/12')).toBe('operation/instance-detail')
    expect(docForPath('/nowhere')).toBe('index')
  })
})

describe('accessibility coverage', () => {
  it('checks every navigable page with axe', async () => {
    const { readFileSync } = await import('node:fs')
    const spec = readFileSync('e2e/a11y.spec.ts', 'utf8') // vitest runs from web/
    for (const r of navRoutes) expect(spec, `e2e/a11y.spec.ts should visit ${r.path}`).toContain(`'${r.path}'`)
  })
})
