/** Left half of the till: one big tile per active product, with today's
 * rate on it. Tapping a tile opens the weight pad — nothing is added to the
 * cart until a weight has actually been entered. */
import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { formatMoney } from '@/lib/money'
import { cn } from '@/lib/utils'
import type { Product } from '@/types'

interface Props {
  products: Product[]
  isLoading: boolean
  error: string | null
  search: string
  onSearchChange: (next: string) => void
  onPick: (product: Product) => void
  /** Focused by the `/` hotkey. */
  searchRef: React.Ref<HTMLInputElement>
}

export function ProductTiles({ products, isLoading, error, search, onSearchChange, onPick, searchRef }: Props) {
  const needle = search.trim().toLowerCase()
  const shown = needle
    ? products.filter(
        (p) => p.name.toLowerCase().includes(needle) || (p.sku ?? '').toLowerCase().includes(needle),
      )
    : products

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          ref={searchRef}
          className="h-11 pl-9"
          placeholder="Search products  ( / )"
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          onKeyDown={(e) => {
            // One match and Enter: ring it, so a keyboard-only cashier never
            // has to reach for the mouse.
            if (e.key === 'Enter' && shown.length === 1) {
              e.preventDefault()
              onPick(shown[0])
            }
          }}
        />
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}

      <div className="min-h-0 flex-1 overflow-y-auto pr-1">
        {isLoading && <p className="text-sm text-muted-foreground">Loading products…</p>}
        {!isLoading && shown.length === 0 && (
          <p className="text-sm text-muted-foreground">
            {products.length === 0
              ? 'No active products. Add one on the Rates screen first.'
              : 'No product matches that search.'}
          </p>
        )}
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-4">
          {shown.map((product) => {
            const rateless = !(product.rate > 0)
            return (
              <button
                key={product.id}
                type="button"
                onClick={() => onPick(product)}
                className={cn(
                  'flex min-h-[104px] flex-col justify-between rounded-xl border border-border bg-card p-4 text-left shadow-sm transition-colors',
                  'hover:border-primary/60 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  rateless && 'border-dashed opacity-70',
                )}
              >
                <span className="line-clamp-2 text-base font-semibold leading-tight">{product.name}</span>
                <span className="mt-2 text-sm text-muted-foreground tabular-nums">
                  {rateless ? 'No rate set' : `${formatMoney(product.rate)} / ${product.unit_label}`}
                </span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
