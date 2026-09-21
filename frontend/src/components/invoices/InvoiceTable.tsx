/**
 * The invoice browser (spec §3, §6.8): every sale the shop has rung, filtered
 * and paginated, with the detail drawer hanging off a row.
 *
 * The date filters are business dates, not timestamps. `GET /invoices`
 * filters on `business_date`, so a row's date is rendered straight off its
 * `YYYY-MM-DD` prefix and the presets are computed in the date domain
 * (dateRange.ts) — running either through `new Date()` re-parses a Karachi
 * calendar date as UTC midnight and shows the previous day to anyone west of
 * Greenwich, including a demo laptop that never had its zone set.
 *
 * Void is offered to everyone. The PIN is the gate, and it is checked
 * server-side against an active admin (`staffpin.AdminOnly`); hiding the
 * button from a counter would only mean a counter with the owner standing
 * next to them cannot undo a mis-rung sale.
 */
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import apiClient from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow, tableInCard } from '@/components/ui/table'
import { InvoiceDrawer } from '@/components/invoices/InvoiceDrawer'
import { useSettings } from '@/components/settings/useSettings'
import { formatMoney } from '@/lib/money'
import { formatBusinessDate, formatTimePK, paymentMethodLabel } from '@/lib/print/format'
import { cn } from '@/lib/utils'
import {
  DATE_PRESETS,
  matchPreset,
  presetLabel,
  presetRange,
  rangeInverted,
  rangeParams,
  type DateRange,
} from '@/components/invoices/dateRange'
import type { InvoiceListParams, InvoiceStatus, PaymentMethod } from '@/types'

const PER_PAGE = 50

export const INVOICES_KEY = ['invoices', 'list'] as const

const TENDERS: readonly PaymentMethod[] = ['cash', 'card', 'online', 'credit'] as const
const STATUSES: readonly InvoiceStatus[] = ['completed', 'voided'] as const

/** Radix Select refuses an empty item value, so "no filter" travels as a
 * sentinel and is stripped before the request rather than sent as `?status=`. */
const ANY = 'all'

/** `off` is the resting state until FBR Digital Invoicing is configured
 * (Phase 7) — it means "not fiscalised", not "failed". */
function fiscalBadge(status: string): { label: string; variant: 'secondary' | 'success' | 'warning' | 'destructive' } {
  switch (status) {
    case 'synced':
      return { label: 'Synced', variant: 'success' }
    case 'pending':
      return { label: 'Pending', variant: 'warning' }
    case 'failed':
      return { label: 'Failed', variant: 'destructive' }
    default:
      return { label: 'Off', variant: 'secondary' }
  }
}

export function InvoiceTable() {
  const { data: settings } = useSettings()
  const boundaryHour = settings?.day_boundary_hour ?? 0

  const [range, setRange] = useState<DateRange>({ from: '', to: '' })
  const [search, setSearch] = useState('')
  const [debounced, setDebounced] = useState('')
  const [tender, setTender] = useState<PaymentMethod | ''>('')
  const [status, setStatus] = useState<InvoiceStatus | ''>('')
  const [page, setPage] = useState(1)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  // The screen lands on today. Settings arrive a tick later than the first
  // render, so the default is applied once they do rather than baked into
  // useState with a boundary hour of 0 that may be wrong.
  const [rangeInitialised, setRangeInitialised] = useState(false)
  useEffect(() => {
    if (rangeInitialised || !settings) return
    const today = presetRange('today', new Date(), settings.day_boundary_hour)
    if (today) setRange(today)
    setRangeInitialised(true)
  }, [settings, rangeInitialised])

  useEffect(() => {
    const t = setTimeout(() => {
      setDebounced(search.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(t)
  }, [search])

  const activePreset = useMemo(() => matchPreset(range, new Date(), boundaryHour), [range, boundaryHour])
  const inverted = rangeInverted(range)

  const params: InvoiceListParams = {
    ...rangeParams(range),
    ...(debounced ? { search: debounced } : {}),
    ...(tender ? { payment_method: tender } : {}),
    ...(status ? { status } : {}),
    page,
    per_page: PER_PAGE,
  }

  const { data, isLoading, error } = useQuery({
    queryKey: [...INVOICES_KEY, params],
    queryFn: () => apiClient.getInvoices(params),
    enabled: rangeInitialised && !inverted,
  })

  const invoices = data?.data ?? []
  const totalPages = data?.meta.total_pages ?? 1
  const total = data?.meta.total ?? 0

  const applyPreset = (preset: (typeof DATE_PRESETS)[number]) => {
    const next = presetRange(preset, new Date(), boundaryHour)
    if (!next) return
    setRange(next)
    setPage(1)
  }

  return (
    <Card>
      <CardHeader>
        <CardDescription className="max-w-3xl">
          Filtered on the business date, so a sale rung after midnight lands on the day it was sold, not the calendar
          date it was typed. Voided invoices stay listed and are excluded from every total.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex gap-2">
            {DATE_PRESETS.map((preset) => (
              <Button
                key={preset}
                size="sm"
                variant="outline"
                aria-pressed={activePreset === preset}
                className={cn(activePreset === preset && 'border-foreground bg-secondary text-foreground')}
                onClick={() => applyPreset(preset)}
              >
                {presetLabel(preset)}
              </Button>
            ))}
          </div>

          <div className="space-y-1">
            <Label htmlFor="invoices-from" className="text-xs text-muted-foreground">
              From
            </Label>
            <Input
              id="invoices-from"
              type="date"
              className="w-[10.5rem]"
              value={range.from}
              onChange={(e) => {
                setRange((r) => ({ ...r, from: e.target.value }))
                setPage(1)
              }}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="invoices-to" className="text-xs text-muted-foreground">
              To
            </Label>
            <Input
              id="invoices-to"
              type="date"
              className="w-[10.5rem]"
              value={range.to}
              onChange={(e) => {
                setRange((r) => ({ ...r, to: e.target.value }))
                setPage(1)
              }}
            />
          </div>

          <div className="relative min-w-[14rem] flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              placeholder="Search invoice number or customer"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="invoices-tender" className="text-xs text-muted-foreground">
              Tender
            </Label>
            <Select
              value={tender === '' ? ANY : tender}
              onValueChange={(v) => {
                setTender(v === ANY ? '' : (v as PaymentMethod))
                setPage(1)
              }}
            >
              <SelectTrigger id="invoices-tender" className="w-[11rem]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ANY}>All tenders</SelectItem>
                {TENDERS.map((t) => (
                  <SelectItem key={t} value={t}>
                    {paymentMethodLabel(t)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1">
            <Label htmlFor="invoices-status" className="text-xs text-muted-foreground">
              Status
            </Label>
            <Select
              value={status === '' ? ANY : status}
              onValueChange={(v) => {
                setStatus(v === ANY ? '' : (v as InvoiceStatus))
                setPage(1)
              }}
            >
              <SelectTrigger id="invoices-status" className="w-[10rem]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ANY}>All statuses</SelectItem>
                {STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s === 'completed' ? 'Completed' : 'Voided'}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        {inverted && <p className="text-sm text-destructive">The From date is after the To date — no sale can match that.</p>}
        {error && <p className="text-sm text-destructive">{error instanceof Error ? error.message : 'Could not load invoices'}</p>}

        <Table className={tableInCard}>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead>Number</TableHead>
              <TableHead>Customer</TableHead>
              <TableHead className="hidden lg:table-cell">Cashier</TableHead>
              <TableHead>Tender</TableHead>
              <TableHead className="text-right">Total payable</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="hidden md:table-cell">FBR</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={8} className="text-center text-muted-foreground">
                  Loading…
                </TableCell>
              </TableRow>
            )}
            {!isLoading && invoices.length === 0 && (
              <TableRow>
                <TableCell colSpan={8} className="text-center text-muted-foreground">
                  No invoices match these filters.
                </TableCell>
              </TableRow>
            )}
            {invoices.map((invoice) => {
              const voided = invoice.status === 'voided'
              const fiscal = fiscalBadge(invoice.fiscal_status)
              return (
                <TableRow
                  key={invoice.id}
                  className={cn('cursor-pointer', voided && 'opacity-60')}
                  onClick={() => setSelectedId(invoice.id)}
                >
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    {formatBusinessDate(invoice.business_date)}
                    <span className="ml-2 tabular-nums">{formatTimePK(invoice.created_at)}</span>
                  </TableCell>
                  <TableCell className="font-medium tabular-nums">{invoice.invoice_number}</TableCell>
                  <TableCell>{invoice.customer_name ?? <span className="text-muted-foreground">Walk-in</span>}</TableCell>
                  <TableCell className="hidden lg:table-cell text-muted-foreground">{invoice.cashier_name}</TableCell>
                  <TableCell>{paymentMethodLabel(invoice.payment_method)}</TableCell>
                  <TableCell className="text-right font-medium tabular-nums">{formatMoney(invoice.total_payable)}</TableCell>
                  <TableCell>
                    {voided ? <Badge variant="destructive">Voided</Badge> : <Badge variant="secondary">Completed</Badge>}
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    <Badge variant={fiscal.variant}>{fiscal.label}</Badge>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>

        <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
          <span className="text-muted-foreground">
            {total} invoice{total === 1 ? '' : 's'}
          </span>
          {totalPages > 1 && (
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                Previous
              </Button>
              <span className="text-muted-foreground">
                Page {page} of {totalPages}
              </span>
              <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
                Next
              </Button>
            </div>
          )}
        </div>
      </CardContent>

      <InvoiceDrawer invoiceId={selectedId} onOpenChange={(open) => !open && setSelectedId(null)} />
    </Card>
  )
}
