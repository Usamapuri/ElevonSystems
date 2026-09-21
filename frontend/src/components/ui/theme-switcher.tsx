import { Sun, Moon, Contrast } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTheme } from '@/contexts/ThemeContext'
import type { ThemePreference } from '@/types'

const OPTIONS: { value: ThemePreference; label: string; icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'high-contrast', label: 'Contrast', icon: Contrast },
]

/**
 * Compact 3-up theme picker. Reads and writes through the global `useTheme()`
 * hook — no local state or persistence of its own. Renders plain buttons (not
 * menu items) so it can live inside a Radix dropdown without the dropdown
 * closing on selection. The whole UI re-themes live on click, so there's
 * deliberately no toast.
 */
export function ThemeSwitcher() {
  const { theme, setTheme } = useTheme()

  return (
    <div className="grid grid-cols-3 gap-1" role="radiogroup" aria-label="Theme">
      {OPTIONS.map((opt) => {
        const active = theme === opt.value
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => setTheme(opt.value)}
            className={cn(
              'flex flex-col items-center gap-1 rounded-md border px-1 py-2 text-[11px] font-semibold transition-colors',
              active
                ? 'border-foreground/30 bg-secondary text-foreground'
                : 'border-transparent text-muted-foreground hover:bg-muted hover:text-foreground',
            )}
          >
            <opt.icon className="h-4 w-4" />
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}
