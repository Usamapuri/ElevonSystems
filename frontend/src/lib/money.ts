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
