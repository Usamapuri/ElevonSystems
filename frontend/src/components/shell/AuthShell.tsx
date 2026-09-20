import { Flame } from 'lucide-react'
import type { ReactNode } from 'react'

/** Centred single-column layout for the password pages. */
export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-6">
      <div className="w-full max-w-sm space-y-6">
        <div className="flex items-center gap-2 text-xl font-bold">
          <Flame className="h-6 w-6 text-primary" /> Elevon POS
        </div>
        {children}
      </div>
    </div>
  )
}
