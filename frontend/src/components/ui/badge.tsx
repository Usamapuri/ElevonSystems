import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

/**
 * Status chips read as instrument labels, not pills: 6px corners, a tinted
 * surface, a hairline in the semantic colour and the semantic ink on top.
 * Every variant is a word — no colour-only states, because a chip that means
 * something only if you can tell green from amber means nothing to a
 * colour-blind cashier on a washed-out counter monitor.
 */
const badgeVariants = cva(
  'inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-bold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
  {
    variants: {
      variant: {
        default:
          'border-transparent bg-primary text-primary-foreground',
        secondary:
          'border-border bg-muted text-muted-foreground',
        destructive:
          'border-destructive/30 bg-destructive-soft text-destructive-ink',
        outline: 'border-border text-foreground',
        success:
          'border-success/30 bg-success-soft text-success-ink',
        warning:
          'border-warning/30 bg-warning-soft text-warning-ink',
        info:
          'border-border bg-secondary text-secondary-foreground',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants }

