/**
 * Who the sale is for. A walk-in is the normal case and needs no account;
 * a credit sale needs one, and the server refuses a credit invoice without
 * it (customer_required) or against an account with credit switched off
 * (credit_not_allowed).
 *
 * The balance and limit are shown at the point of choosing because that is
 * where they matter: a cashier who can see "owes Rs 42,000 of Rs 50,000"
 * before ringing the sale does not get surprised by a credit_limit_exceeded
 * at the tender dialog.
 *
 * The customer also decides further tax: an unregistered buyer (and a
 * walk-in, who is unregistered by definition) attracts it, a Registered one
 * does not (§7.5) — so changing this changes the totals, live.
 */
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search, User, UserX, X } from 'lucide-react'
import apiClient from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Money } from '@/components/shared/Money'
import { cn } from '@/lib/utils'
import type { Customer } from '@/types'

interface Props {
  value: Customer | null
  onChange: (customer: Customer | null) => void
  /** Credit tender: the sale cannot be settled without an account. */
  required?: boolean
}

export function CustomerPicker({ value, onChange, required = false }: Props) {
  const [open, setOpen] = useState(false)

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium">Customer</span>
        {value && (
          <button
            type="button"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
            onClick={() => onChange(null)}
          >
            <X className="h-3 w-3" /> Walk-in
          </button>
        )}
      </div>

      <Button
        type="button"
        variant="outline"
        className={cn(
          'h-auto w-full justify-start px-3 py-2 text-left',
          required && !value && 'border-destructive text-destructive',
        )}
        onClick={() => setOpen(true)}
      >
        {value ? (
          <span className="flex min-w-0 flex-1 items-center gap-2">
            <User className="h-4 w-4 shrink-0" />
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">{value.name}</span>
              <span className="block truncate text-xs font-normal text-muted-foreground">
                Balance <Money amount={value.balance} />
                {value.credit_allowed
                  ? value.credit_limit === null
                    ? ' · no credit limit'
                    : ` of ${value.credit_limit.toLocaleString('en-PK')}`
                  : ' · no credit'}
              </span>
            </span>
          </span>
        ) : (
          <span className="flex items-center gap-2 text-muted-foreground">
            <UserX className="h-4 w-4" />
            {required ? 'A credit sale needs an account — pick one' : 'Walk-in (no account)'}
          </span>
        )}
      </Button>

      <CustomerSearchDialog
        open={open}
        onOpenChange={setOpen}
        onPick={(customer) => {
          onChange(customer)
          setOpen(false)
        }}
      />
    </div>
  )
}

function CustomerSearchDialog({
  open,
  onOpenChange,
  onPick,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onPick: (customer: Customer | null) => void
}) {
  const [search, setSearch] = useState('')
  const [debounced, setDebounced] = useState('')

  useEffect(() => {
    const t = setTimeout(() => setDebounced(search.trim()), 250)
    return () => clearTimeout(t)
  }, [search])

  useEffect(() => {
    if (!open) {
      setSearch('')
      setDebounced('')
    }
  }, [open])

  const { data, isLoading, error } = useQuery({
    queryKey: ['customers', 'pos', debounced],
    queryFn: () => apiClient.getCustomers({ search: debounced || undefined, active: true, per_page: 20 }),
    enabled: open,
  })
  const customers = data?.data ?? []

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Customer</DialogTitle>
          <DialogDescription>Search by name or phone. Leave it as a walk-in for a cash sale.</DialogDescription>
        </DialogHeader>

        <div className="relative">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            autoFocus
            className="h-11 pl-9"
            placeholder="Name or phone"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>

        {error && <p className="text-sm text-destructive">Could not load customers</p>}

        <div className="max-h-[45vh] space-y-2 overflow-y-auto pr-1">
          <button
            type="button"
            className="w-full rounded-lg border border-border px-3 py-2 text-left text-sm hover:bg-accent"
            onClick={() => onPick(null)}
          >
            <span className="flex items-center gap-2 text-muted-foreground">
              <UserX className="h-4 w-4" /> Walk-in (no account)
            </span>
          </button>

          {isLoading && <p className="px-1 text-sm text-muted-foreground">Searching…</p>}
          {!isLoading && customers.length === 0 && (
            <p className="px-1 text-sm text-muted-foreground">No customer matches.</p>
          )}

          {customers.map((customer) => (
            <button
              key={customer.id}
              type="button"
              className="w-full rounded-lg border border-border px-3 py-2 text-left hover:bg-accent"
              onClick={() => onPick(customer)}
            >
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate font-medium">{customer.name}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {customer.phone ?? 'No phone'} · {customer.buyer_registration_type}
                  </p>
                </div>
                <div className="shrink-0 text-right text-xs">
                  <p className={cn('font-medium', customer.balance > 0 && 'text-amber-600')}>
                    <Money amount={customer.balance} />
                  </p>
                  {customer.credit_allowed ? (
                    <Badge variant="outline" className="mt-1">
                      {customer.credit_limit === null ? 'No limit' : `Limit ${customer.credit_limit.toLocaleString('en-PK')}`}
                    </Badge>
                  ) : (
                    <span className="text-muted-foreground">No credit</span>
                  )}
                </div>
              </div>
            </button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
