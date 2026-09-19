import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import type { ThemePreference } from '@/types'

const STORAGE_KEY = 'elevon-theme'
const DEFAULT_THEME: ThemePreference = 'light' // never the OS preference: a dark laptop still opens the till bright
const VALID: ThemePreference[] = ['light', 'dark', 'high-contrast']

function isTheme(v: unknown): v is ThemePreference {
  return typeof v === 'string' && (VALID as string[]).includes(v)
}

function applyTheme(theme: ThemePreference) {
  const root = document.documentElement
  root.classList.remove('dark', 'high-contrast')
  if (theme !== 'light') root.classList.add(theme)
}

interface ThemeContextValue {
  theme: ThemePreference
  setTheme: (t: ThemePreference) => void
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemePreference>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY)
      return isTheme(stored) ? stored : DEFAULT_THEME
    } catch {
      return DEFAULT_THEME
    }
  })

  useEffect(() => applyTheme(theme), [theme])

  const setTheme = (t: ThemePreference) => {
    if (!isTheme(t)) return
    setThemeState(t)
    try {
      localStorage.setItem(STORAGE_KEY, t)
    } catch {
      /* private mode: in-memory only */
    }
  }

  return <ThemeContext.Provider value={{ theme, setTheme }}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider')
  return ctx
}
