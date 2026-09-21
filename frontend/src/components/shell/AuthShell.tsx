import type { ReactNode } from 'react'
import { BrandMark } from '@/components/shell/BrandMark'

/** Centred single-column layout for the password pages. */
export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-6">
      <div className="w-full max-w-sm space-y-6">
        <div className="flex items-center gap-3 text-lg font-extrabold tracking-tight">
          <BrandMark /> Elevon POS
        </div>
        {children}
      </div>
    </div>
  )
}
