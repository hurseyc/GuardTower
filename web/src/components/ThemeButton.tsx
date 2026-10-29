import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { Check } from 'lucide-react'
import { Toaster } from 'sonner'
import { setTheme, themeIcons, themeOptions, useTheme, type ThemePref } from '@/lib/theme'

/** Header control: Light, Dark or Auto (follows the operating system). */
export function ThemeButton() {
  const { pref, dark } = useTheme()
  const current = themeOptions.find((o) => o.value === pref)!
  const Icon = themeIcons[pref]
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger
        className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm text-muted hover:bg-hover hover:text-fg"
        aria-label={`Theme: ${current.label}${pref === 'system' ? ` (${dark ? 'dark' : 'light'} now)` : ''}`}
        title={`Theme: ${current.label}`}
      >
        <Icon className="h-4 w-4" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={6} className="z-50 min-w-44 rounded-xl border border-border bg-elevated p-1 shadow-pop">
          <DropdownMenu.RadioGroup value={pref} onValueChange={(v) => setTheme(v as ThemePref)}>
            {themeOptions.map((o) => {
              const I = themeIcons[o.value]
              return (
                <DropdownMenu.RadioItem
                  key={o.value}
                  value={o.value}
                  className="flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-2 text-sm outline-none data-[highlighted]:bg-hover"
                >
                  <I className="h-4 w-4 text-muted" aria-hidden />
                  <span className="flex-1">
                    {o.label}
                    {o.value === 'system' && <span className="ml-1 text-xs text-faint">(system)</span>}
                  </span>
                  <DropdownMenu.ItemIndicator>
                    <Check className="h-4 w-4 text-accent" aria-hidden />
                  </DropdownMenu.ItemIndicator>
                </DropdownMenu.RadioItem>
              )
            })}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}

/** Toasts follow the chosen theme, not only the operating system's. */
export function ThemedToaster() {
  const { dark } = useTheme()
  return <Toaster position="bottom-right" theme={dark ? 'dark' : 'light'} richColors closeButton />
}
