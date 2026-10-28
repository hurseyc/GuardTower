import { useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import MiniSearch from 'minisearch'
import { BookOpen, Search } from 'lucide-react'
import { api, type DocPage } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Card, Input, Skeleton } from '@/components/ui'
import { Markdown } from '@/components/Markdown'
import { slugify } from '@/lib/slugify'

export function DocsPage() {
  const params = useParams()
  const slug = params['*'] || 'index'
  const loc = useLocation()
  const [q, setQ] = useState('')
  const { data: pages } = useQuery({ queryKey: ['docs-full'], queryFn: () => api.get<DocPage[]>('/docs?full=1'), staleTime: 60_000 })
  const { data: meta } = useQuery({ queryKey: ['meta'], queryFn: () => api.get<{ version: string }>('/meta'), staleTime: Infinity })
  const page = pages?.find((p) => p.slug === slug)

  const index = useMemo(() => {
    const ms = new MiniSearch<DocPage>({
      idField: 'slug',
      fields: ['title', 'summary', 'body', 'section'],
      storeFields: ['title', 'section', 'summary'],
      searchOptions: { boost: { title: 3, summary: 2 }, prefix: true, fuzzy: 0.2 },
    })
    if (pages) ms.addAll(pages)
    return ms
  }, [pages])
  const results = q.trim() ? index.search(q).slice(0, 12) : []

  useEffect(() => {
    if (loc.hash) {
      const el = document.getElementById(decodeURIComponent(loc.hash.slice(1)))
      el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    } else {
      document.querySelector('main')?.scrollTo({ top: 0 })
    }
  }, [loc.hash, slug, page])

  const sections = useMemo(() => {
    const m = new Map<string, DocPage[]>()
    for (const p of pages ?? []) m.set(p.section || 'Other', [...(m.get(p.section || 'Other') ?? []), p])
    return [...m.entries()]
  }, [pages])

  const toc = useMemo(() => (page?.body ?? '').split('\n').filter((l) => l.startsWith('## ')).map((l) => l.slice(3).trim()), [page])

  return (
    <div className="mx-auto flex max-w-7xl gap-8 px-6 py-6">
      <aside className="hidden w-60 shrink-0 lg:block">
        <div className="sticky top-6 space-y-5">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-faint" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search docs…" className="pl-9" />
          </div>
          {q.trim() ? (
            <div className="space-y-1">
              {results.length === 0 && <p className="px-2 text-sm text-faint">No matches.</p>}
              {results.map((r) => (
                <Link key={r.id} to={`/docs/${r.id}`} onClick={() => setQ('')} className="block rounded-lg px-2 py-1.5 hover:bg-hover">
                  <div className="text-sm font-medium">{r.title}</div>
                  <div className="text-xs text-faint">{r.section}</div>
                </Link>
              ))}
            </div>
          ) : (
            sections.map(([section, ps]) => (
              <div key={section}>
                <div className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-wider text-faint">{section}</div>
                {ps.map((p) => (
                  <Link
                    key={p.slug}
                    to={`/docs/${p.slug}`}
                    className={cn('block rounded-lg px-2 py-1 text-sm', p.slug === slug ? 'bg-accent-soft font-medium text-accent' : 'text-muted hover:bg-hover hover:text-fg')}
                  >
                    {p.title}
                  </Link>
                ))}
              </div>
            ))
          )}
        </div>
      </aside>
      <article className="min-w-0 flex-1">
        <div className="mb-4 lg:hidden">
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search docs…" />
          {results.map((r) => (
            <Link key={r.id} to={`/docs/${r.id}`} onClick={() => setQ('')} className="block border-b border-border px-2 py-2 text-sm">
              {r.title}
            </Link>
          ))}
        </div>
        {!pages ? (
          <div className="space-y-3">
            <Skeleton className="h-8 w-1/2" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-5/6" />
          </div>
        ) : page ? (
          <Card className="px-8 py-7">
            {page.section && <div className="mb-2 text-xs font-medium uppercase tracking-wider text-accent">{page.section}</div>}
            <Markdown source={page.body ?? ''} baseSlug={slug} />
            <div className="mt-10 flex items-center justify-between border-t border-border pt-4 text-xs text-faint">
              <span>Documentation for GuardTower {meta?.version}</span>
              <a className="hover:text-fg" href={`https://github.com/hurseyc/GuardTower/edit/main/docs/${slug}.md`} target="_blank" rel="noreferrer noopener">
                Edit this page on GitHub
              </a>
            </div>
          </Card>
        ) : (
          <Card className="p-10 text-center">
            <BookOpen className="mx-auto h-8 w-8 text-faint" />
            <p className="mt-3 text-sm text-muted">Page not found.</p>
            <Link to="/docs" className="mt-2 inline-block text-sm text-accent">
              Go to the documentation home
            </Link>
          </Card>
        )}
      </article>
      {toc.length > 2 && (
        <nav className="hidden w-48 shrink-0 xl:block">
          <div className="sticky top-6">
            <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-faint">On this page</div>
            {toc.map((h) => (
              <a key={h} href={`#${slugify(h)}`} className="block py-1 text-xs text-muted hover:text-fg">
                {h}
              </a>
            ))}
          </div>
        </nav>
      )}
    </div>
  )
}
