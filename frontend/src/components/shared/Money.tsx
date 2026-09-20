/** Rupee amount, tabular so a column of them lines up. One place to change
 * if money display ever changes; formatMoney stays the single formatter. */
import { formatMoney } from '@/lib/money'
import { cn } from '@/lib/utils'

interface Props {
  amount: number
  className?: string
}

export function Money({ amount, className }: Props) {
  return <span className={cn('tabular-nums', className)}>{formatMoney(amount)}</span>
}
