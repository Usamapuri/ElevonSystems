/**
 * The flame in its orange tile — the one mark this application has.
 *
 * It lives in its own module rather than on the Sidebar so the sign-in and
 * password screens can wear it without pulling the whole authenticated shell
 * (and its API client) into their bundle.
 */
import { Flame } from 'lucide-react'
import { cn } from '@/lib/utils'

export function BrandMark({ className }: { className?: string }) {
  return (
    <span className={cn('flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary', className)}>
      <Flame className="h-5 w-5 text-primary-foreground" aria-hidden />
    </span>
  )
}
