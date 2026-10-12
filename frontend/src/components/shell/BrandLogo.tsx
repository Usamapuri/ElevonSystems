/**
 * The store's mark: its uploaded logo when there is one, else the flame
 * tile. Same footprint either way so the rail and the sign-in page do not
 * shift when a logo is added.
 */
import { BrandMark } from '@/components/shell/BrandMark'
import { cn } from '@/lib/utils'

export function BrandLogo({ logo, className }: { logo: string; className?: string }) {
  if (!logo) return <BrandMark className={className} />
  return (
    <span className={cn('flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-md bg-white', className)}>
      <img src={logo} alt="" className="h-full w-full object-contain" />
    </span>
  )
}
