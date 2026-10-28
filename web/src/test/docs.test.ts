// Docs freshness gate: keeps the in-app wiki in step with the UI.
// Fails when a page's help link points at a missing doc, a doc links to a
// missing page/anchor, or a doc lacks front matter.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, dirname, relative, normalize } from 'node:path'
import { describe, expect, it } from 'vitest'
import { navRoutes, otherRoutes } from '@/lib/routes'

const docsRoot = join(__dirname, '..', '..', '..', 'docs')

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f)
    return statSync(p).isDirectory() ? walk(p) : p.endsWith('.md') ? [p] : []
  })
}

const files = walk(docsRoot).filter((f) => !relative(docsRoot, f).startsWith('dev'))
const slugs = new Set(files.map((f) => relative(docsRoot, f).replace(/\.md$/, '')))

function anchors(md: string): Set<string> {
  const out = new Set<string>()
  for (const line of md.split('\n')) {
    const m = /^#{2,3}\s+(.*)$/.exec(line)
    if (m) out.add(m[1].toLowerCase().replace(/[^a-z0-9\s-]/g, '').trim().replace(/\s+/g, '-'))
  }
  return out
}

describe('documentation', () => {
  it('has a page for every route', () => {
    for (const r of [...navRoutes, ...otherRoutes]) {
      expect(slugs.has(r.doc), `route ${r.path} -> docs/${r.doc}.md`).toBe(true)
    }
  })

  for (const file of files) {
    const rel = relative(docsRoot, file)
    const src = readFileSync(file, 'utf8')
    it(`${rel} has front matter`, () => {
      expect(src.startsWith('---\n'), 'starts with ---').toBe(true)
      expect(/\ntitle: .+/.test(src), 'title').toBe(true)
      expect(/\nsection: .+/.test(src), 'section').toBe(true)
      expect(/\nsummary: .+/.test(src), 'summary').toBe(true)
    })
    it(`${rel} has no broken internal links`, () => {
      const links = [...src.matchAll(/\]\(([^)\s]+)\)/g)].map((m) => m[1]).filter((l) => !/^[a-z]+:/i.test(l))
      for (const link of links) {
        const [p, hash] = link.split('#')
        if (!p) {
          if (hash) expect(anchors(src).has(hash), `${rel}: #${hash}`).toBe(true)
          continue
        }
        const target = normalize(join(dirname(rel), p)).replace(/\.md$/, '')
        expect(slugs.has(target), `${rel}: link to ${link}`).toBe(true)
        if (hash) {
          const tsrc = readFileSync(join(docsRoot, target + '.md'), 'utf8')
          expect(anchors(tsrc).has(hash), `${rel}: anchor ${link}`).toBe(true)
        }
      }
    })
  }
})
