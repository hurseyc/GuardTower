import { describe, expect, it } from 'vitest'
import { docForPath } from './routes'

describe('docForPath', () => {
  it('maps exact and parameterised routes', () => {
    expect(docForPath('/')).toBe('operation/dashboard')
    expect(docForPath('/instances')).toBe('setup/adding-instances')
    expect(docForPath('/instances/12')).toBe('operation/instance-detail')
    expect(docForPath('/nowhere')).toBe('index')
  })
})
