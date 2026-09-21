/** Rupee display. Whole rupees show no decimals; paisa show two. */
export function formatMoney(amount: number): string {
  const abs = Math.abs(amount)
  const paisa = Math.round(abs * 100) % 100
  const body = abs.toLocaleString('en-PK', {
    minimumFractionDigits: paisa === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  })
  return `${amount < 0 ? '-' : ''}Rs ${body}`
}

/** Weight display: always 3 decimals, kg unit. */
export function formatKg(kg: number): string {
  return `${kg.toLocaleString('en-PK', { minimumFractionDigits: 3, maximumFractionDigits: 3 })} kg`
}

/** Weight for a chart axis tick: whole kilos, no unit — `formatKg`'s 3
 * decimals make a Y-axis (`TrendChart`) wider than the plot it labels.
 * Kept separate from `formatKg` rather than a formatting option on it: the
 * tooltip next to the same axis still wants the exact 3-decimal figure. */
export function formatKgTick(kg: number): string {
  return Math.round(kg).toLocaleString('en-PK')
}

/** Auto-compact rupee figure for tight spaces (heatmap cells, stat deltas):
 * 1,284 stays whole; 12,900 becomes 12.9K; 4,200,000 becomes 4.2M. Always
 * whole rupees, no paisa — a compact label is not where paisa is checked. */
export function compactMoney(amount: number): string {
  const abs = Math.abs(amount)
  const sign = amount < 0 ? '-' : ''
  if (abs < 1000) return `${sign}${Math.round(abs).toLocaleString('en-PK')}`
  const divisor = abs >= 1_000_000 ? 1_000_000 : 1000
  const suffix = abs >= 1_000_000 ? 'M' : 'K'
  return `${sign}${(abs / divisor).toFixed(1)}${suffix}`
}
