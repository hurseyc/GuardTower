import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { cn } from '@/lib/utils'

/** An instance's name that opens its dashboard. Falls back to plain text when the id is unknown. */
export function InstanceLink({ id, children, className }: { id?: number | null; children: ReactNode; className?: string }) {
  if (!id) return <span className={className}>{children}</span>
  return (
    <Link
      to={`/instances/${id}`}
      onClick={(e) => e.stopPropagation()}
      className={cn('underline-offset-2 hover:text-accent hover:underline focus-visible:underline', className)}
    >
      {children}
    </Link>
  )
}
