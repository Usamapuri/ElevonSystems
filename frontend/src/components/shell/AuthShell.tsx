import type { ReactNode } from 'react'
import { BrandLogo } from '@/components/shell/BrandLogo'
import { browserStorage, recallBrand } from '@/lib/brand'

/** Centred single-column layout for the password pages. Wears the brand
 * this browser last saw for the store (no API before sign-in). */
export function AuthShell({ children }: { children: ReactNode }) {
  const brand = recallBrand(browserStorage())
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-6">
      <div className="w-full max-w-sm space-y-6">
        <div className="flex items-center gap-3 text-lg font-extrabold tracking-tight">
          <BrandLogo logo={brand.logo} /> {brand.name}
        </div>
        {children}
      </div>
    </div>
  )
}
