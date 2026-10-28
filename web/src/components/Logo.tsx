export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <defs>
        <linearGradient id="gt-logo" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#34d399" />
          <stop offset="1" stopColor="#0ea5e9" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" className="fill-subtle" />
      <path d="M16 4.5 7 8.2v6.4c0 6 3.8 10.6 9 12.9 5.2-2.3 9-6.9 9-12.9V8.2L16 4.5Z" fill="none" stroke="url(#gt-logo)" strokeWidth="2.2" strokeLinejoin="round" />
      <path d="M13 22v-7h6v7M12 15h8M14 15v-3h4v3" fill="none" stroke="url(#gt-logo)" strokeWidth="1.8" strokeLinejoin="round" />
    </svg>
  )
}
