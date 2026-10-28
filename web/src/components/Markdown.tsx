import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Link } from 'react-router'

function slugify(text: string) {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, '')
    .trim()
    .replace(/\s+/g, '-')
}

function textOf(children: React.ReactNode): string {
  if (typeof children === 'string' || typeof children === 'number') return String(children)
  if (Array.isArray(children)) return children.map(textOf).join('')
  if (children && typeof children === 'object' && 'props' in children) {
    return textOf((children as { props: { children?: React.ReactNode } }).props.children)
  }
  return ''
}

/**
 * Renders wiki markdown. Relative links like `../setup/docker.md#tls` are
 * rewritten to in-app routes (/docs/setup/docker#tls).
 */
export function Markdown({ source, baseSlug, onNavigate }: { source: string; baseSlug: string; onNavigate?: () => void }) {
  return (
    <div className="prose-gt">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          h2: ({ children }) => <h2 id={slugify(textOf(children))}>{children}</h2>,
          h3: ({ children }) => <h3 id={slugify(textOf(children))}>{children}</h3>,
          a: ({ href = '', children }) => {
            if (/^[a-z]+:/i.test(href)) {
              return (
                <a href={href} target="_blank" rel="noreferrer noopener">
                  {children}
                </a>
              )
            }
            if (href.startsWith('#')) return <a href={href}>{children}</a>
            const [p, hash] = href.split('#')
            const baseDir = baseSlug.includes('/') ? baseSlug.slice(0, baseSlug.lastIndexOf('/') + 1) : ''
            const resolved = resolvePath(baseDir + p.replace(/\.md$/, ''))
            return (
              <Link to={`/docs/${resolved}${hash ? '#' + hash : ''}`} onClick={onNavigate}>
                {children}
              </Link>
            )
          },
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}

function resolvePath(p: string): string {
  const out: string[] = []
  for (const seg of p.split('/')) {
    if (seg === '..') out.pop()
    else if (seg && seg !== '.') out.push(seg)
  }
  return out.join('/') || 'index'
}

export { slugify }
