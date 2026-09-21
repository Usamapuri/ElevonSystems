/**
 * The day-close screen (spec §6.7). One screen, four states, driven entirely
 * by `GET /day/current`:
 *
 *   no row            → declare the float and start the day
 *   open / reopened   → what the drawer should hold, the movements, and the
 *                       count that seals it
 *   closed (today)    → the Z-report, with Reopen behind an admin PIN
 *   open, other date  → a banner: that day has to be closed before today can
 *                       start, which is the `previous_day_open` 409 the till
 *                       would otherwise hit mid-sale
 *
 * A counter can open, record movements and close — that is the job (spec §3).
 * Reopen and force close are admin-only in routes.go and the buttons are
 * hidden for a counter; the PIN is what actually authorises them, and the
 * server checks it against an active admin either way.
 *
 * Every PIN modal and every dialog refuses to close while its request is in
 * flight, the same rule the till follows: dismissing a live attempt is how a
 * half-applied day operation happens.
 */
import { useMemo, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, ArrowDownToLine, ArrowUpFromLine, CalendarClock, Loader2, Sunrise, Unlock } from 'lucide-react'
import apiClient from '@/api/client'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader } from '@/components/shell/PageHeader'
import { PinEntryModal } from '@/components/shared/PinEntryModal'
import { CloseDayForm } from '@/components/dayclose/CloseDayForm'
import { MovementDialog, type MovementType } from '@/components/dayclose/MovementDialog'
import { OpenDayDialog } from '@/components/dayclose/OpenDayDialog'
import { ZReportView } from '@/components/dayclose/ZReportView'
import { useSettings } from '@/components/settings/useSettings'
import { businessDateKey, dayDateKey } from '@/components/pos/dayGate'
import { DEFAULT_VARIANCE_THRESHOLD, normalizeThreshold } from '@/components/dayclose/dayClose'
import { toast } from '@/hooks/use-toast'
import { formatMoney } from '@/lib/money'
import { formatBusinessDate, formatTimePK } from '@/lib/print/format'
import { printZReport } from '@/lib/print/printInvoice'
import { cn } from '@/lib/utils'
import type { CloseDayRequest } from '@/types'

export const Route = createFileRoute('/_app/day-close')({ component: DayClosePage })

const DAY_CURRENT_KEY = ['day', 'current'] as const

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

function DayClosePage() {
  const { user } = Route.useRouteContext()
  const qc = useQueryClient()
  const isAdmin = user.role === 'admin'

  const [openDialog, setOpenDialog] = useState(false)
  const [openError, setOpenError] = useState<string | null>(null)
  const [movementOpen, setMovementOpen] = useState(false)
  const [movementType, setMovementType] = useState<MovementType>('paid_in')
  const [movementError, setMovementError] = useState<string | null>(null)
  const [closeError, setCloseError] = useState<string | null>(null)
  const [reopenOpen, setReopenOpen] = useState(false)
  const [forceOpen, setForceOpen] = useState(false)
  const [pinError, setPinError] = useState<string | null>(null)
  const [printing, setPrinting] = useState(false)

  const { data: settings } = useSettings()
  const threshold = normalizeThreshold(settings?.day_close_variance_threshold ?? DEFAULT_VARIANCE_THRESHOLD)

  const current = useQuery({
    queryKey: DAY_CURRENT_KEY,
    queryFn: async () => {
      const res = await apiClient.getDayCurrent()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the business day')
      return res.data
    },
    refetchOnWindowFocus: true,
  })

  const day = current.data?.day ?? null
  const expected = current.data?.expected ?? null
  const movements = current.data?.movements ?? []
  const dayIsOpen = !!day && (day.status === 'open' || day.status === 'reopened')

  const todayKey = businessDateKey(new Date(), settings?.day_boundary_hour ?? 0)
  const dayKey = day ? dayDateKey(day) : null
  const isPreviousDayOpen = dayIsOpen && dayKey !== todayKey

  /** A sealed day loads its Z-report; the sealed figures live on the row and
   * the endpoint returns them alongside the live recomputation. */
  const zReport = useQuery({
    queryKey: ['day', 'z', day?.id ?? 'none'],
    queryFn: async () => {
      const res = await apiClient.getZReport(day?.id as string)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the Z-report')
      return res.data
    },
    enabled: !!day && !dayIsOpen,
  })

  const refreshDay = () => {
    qc.invalidateQueries({ queryKey: DAY_CURRENT_KEY })
    qc.invalidateQueries({ queryKey: ['day', 'z'] })
  }

  const openDay = useMutation({
    mutationFn: (v: { opening_cash: number; notes: string }) =>
      apiClient.openDay({ opening_cash: v.opening_cash, ...(v.notes ? { notes: v.notes } : {}) }),
    onSuccess: () => {
      setOpenDialog(false)
      setOpenError(null)
      refreshDay()
      toast({ title: 'Business day started', variant: 'success' })
    },
    onError: (err: unknown) => setOpenError(errorMessage(err, 'Could not start the day')),
  })

  const addMovement = useMutation({
    mutationFn: (v: { type: MovementType; amount: number; reason: string; notes: string }) =>
      apiClient.addMovement({ type: v.type, amount: v.amount, reason: v.reason, ...(v.notes ? { notes: v.notes } : {}) }),
    onSuccess: (_res, v) => {
      setMovementOpen(false)
      setMovementError(null)
      refreshDay()
      toast({ title: v.type === 'paid_in' ? 'Money in recorded' : 'Money out recorded', variant: 'success' })
    },
    onError: (err: unknown) => setMovementError(errorMessage(err, 'Could not record the movement')),
  })

  const closeDay = useMutation({
    mutationFn: (payload: CloseDayRequest) => apiClient.closeDay(payload),
    onSuccess: () => {
      setCloseError(null)
      refreshDay()
      toast({ title: 'Business day closed', variant: 'success' })
    },
    onError: (err: unknown) => setCloseError(errorMessage(err, 'Could not close the day')),
  })

  const reopenDay = useMutation({
    mutationFn: (pin: string) => apiClient.reopenDay({ pin }),
    onSuccess: () => {
      setReopenOpen(false)
      setPinError(null)
      refreshDay()
      toast({ title: 'Business day reopened', variant: 'success' })
    },
    onError: (err: unknown) => setPinError(errorMessage(err, 'Could not reopen the day')),
  })

  const forceClose = useMutation({
    mutationFn: (v: { pin: string; reason: string }) => apiClient.forceCloseDay(v),
    onSuccess: () => {
      setForceOpen(false)
      setPinError(null)
      refreshDay()
      toast({ title: 'Business day force-closed', variant: 'success' })
    },
    onError: (err: unknown) => setPinError(errorMessage(err, 'Could not force-close the day')),
  })

  const tiles = useMemo(
    () =>
      expected
        ? [
            { label: 'Expected cash', value: formatMoney(expected.cash), hint: 'Float + cash sales + receipts + in − out' },
            { label: 'Expected card', value: formatMoney(expected.card), hint: 'Card sales and receipts' },
            { label: 'Expected online', value: formatMoney(expected.online), hint: 'Online sales and receipts' },
            { label: 'On account', value: formatMoney(expected.on_account_sales), hint: 'Credit sales — not counted' },
            { label: 'Receipts collected', value: formatMoney(expected.receipts_collected), hint: 'Payments against account' },
          ]
        : [],
    [expected],
  )

  const printZ = async () => {
    if (!settings || !zReport.data) return
    setPrinting(true)
    try {
      await printZReport(zReport.data, settings)
    } finally {
      setPrinting(false)
    }
  }

  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader
        title="Day close"
        description={
          day
            ? `${formatBusinessDate(day.business_date)}, opened ${formatTimePK(day.opened_at)}`
            : 'No business day yet'
        }
      />

      {current.error && (
        <p className="text-sm font-semibold text-destructive">{errorMessage(current.error, 'Could not load the business day')}</p>
      )}
      {current.isLoading && (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading the business day…
        </p>
      )}

      {isPreviousDayOpen && (
        <div className="flex flex-col gap-3 rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-start gap-3">
            <CalendarClock className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />
            <div>
              <p className="font-medium">{formatBusinessDate(dayKey)} is still open.</p>
              <p className="text-muted-foreground">
                Count and close it before today can start — an invoice cannot be dated into a day that is still counting.
              </p>
            </div>
          </div>
          {isAdmin && (
            <Button
              variant="outline"
              size="sm"
              className="shrink-0"
              onClick={() => {
                setPinError(null)
                setForceOpen(true)
              }}
            >
              <AlertTriangle className="mr-2 h-4 w-4" /> Force close it
            </Button>
          )}
        </div>
      )}

      {!current.isLoading && !day && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Sunrise className="h-5 w-5" /> Start the day
            </CardTitle>
            <CardDescription>
              Nothing can be sold until someone declares what is in the drawer. The till stays blocked on purpose: an
              invented opening float makes every cash figure for the rest of the day a fiction.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button
              size="lg"
              onClick={() => {
                setOpenError(null)
                setOpenDialog(true)
              }}
            >
              Declare the float and start
            </Button>
          </CardContent>
        </Card>
      )}

      {dayIsOpen && (
        <>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
            {tiles.map((tile) => (
              <div key={tile.label} className="rounded-lg border border-border bg-card p-4">
                <p className="text-xs font-semibold text-muted-foreground">{tile.label}</p>
                <p className="tabular mt-1 text-xl font-bold">{tile.value}</p>
                <p className="mt-1 text-xs text-muted-foreground">{tile.hint}</p>
              </div>
            ))}
          </div>

          <Card>
            <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:space-y-0">
              <div>
                <CardTitle>Cash movements</CardTitle>
                <CardDescription>
                  Money in or out of the drawer without a sale. Opening float {formatMoney(day.opening_cash)}.
                </CardDescription>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  onClick={() => {
                    setMovementError(null)
                    setMovementType('paid_in')
                    setMovementOpen(true)
                  }}
                >
                  <ArrowDownToLine className="mr-2 h-4 w-4" /> Paid in
                </Button>
                <Button
                  variant="outline"
                  onClick={() => {
                    setMovementError(null)
                    setMovementType('paid_out')
                    setMovementOpen(true)
                  }}
                >
                  <ArrowUpFromLine className="mr-2 h-4 w-4" /> Paid out
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              {movements.length === 0 ? (
                <p className="text-sm text-muted-foreground">Nothing has moved in or out of the drawer today.</p>
              ) : (
                <ul className="space-y-1 text-sm">
                  {movements.map((m) => (
                    <li
                      key={m.id}
                      className="flex flex-wrap items-baseline justify-between gap-2 border-b border-border py-1.5 last:border-b-0"
                    >
                      <span className="min-w-0">
                        <span className="tabular text-muted-foreground">{formatTimePK(m.created_at)}</span> {m.reason}
                        {m.created_by_name ? <span className="text-muted-foreground"> — {m.created_by_name}</span> : null}
                        {m.notes ? <span className="block text-xs text-muted-foreground">{m.notes}</span> : null}
                      </span>
                      <span
                        className={cn(
                          'tabular font-semibold',
                          m.movement_type === 'paid_out' ? 'text-destructive' : 'text-success-ink',
                        )}
                      >
                        {m.movement_type === 'paid_out' ? '-' : '+'}
                        {formatMoney(m.amount)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          <CloseDayForm
            expected={expected}
            threshold={threshold}
            pending={closeDay.isPending}
            error={closeError}
            onSubmit={(payload) => {
              setCloseError(null)
              closeDay.mutate(payload)
            }}
          />
        </>
      )}

      {day && !dayIsOpen && (
        <>
          {zReport.isLoading && (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> Loading the Z-report…
            </p>
          )}
          {zReport.error && (
            <p className="text-sm text-destructive">{errorMessage(zReport.error, 'Could not load the Z-report')}</p>
          )}
          {zReport.data && (
            <ZReportView
              z={zReport.data}
              settings={settings}
              printing={printing}
              onPrint={printZ}
              actions={
                isAdmin ? (
                  <Button
                    variant="outline"
                    onClick={() => {
                      setPinError(null)
                      setReopenOpen(true)
                    }}
                  >
                    <Unlock className="mr-2 h-4 w-4" /> Reopen
                  </Button>
                ) : null
              }
            />
          )}
          <p className="text-sm text-muted-foreground">
            The day is sealed. A sale rung now reopens it for a late sale and writes an audit entry — re-close it
            afterwards so the figures are sealed against what actually happened.
          </p>
        </>
      )}

      <OpenDayDialog
        open={openDialog}
        onOpenChange={(next) => {
          setOpenDialog(next)
          if (!next) setOpenError(null)
        }}
        pending={openDay.isPending}
        error={openError}
        onSubmit={(opening_cash, notes) => {
          setOpenError(null)
          openDay.mutate({ opening_cash, notes })
        }}
      />

      <MovementDialog
        open={movementOpen}
        onOpenChange={(next) => {
          setMovementOpen(next)
          if (!next) setMovementError(null)
        }}
        defaultType={movementType}
        pending={addMovement.isPending}
        error={movementError}
        onSubmit={(movement) => {
          setMovementError(null)
          addMovement.mutate(movement)
        }}
      />

      <PinEntryModal
        open={reopenOpen}
        onOpenChange={(next) => {
          if (reopenDay.isPending) return
          setReopenOpen(next)
          if (!next) setPinError(null)
        }}
        title="Reopen the day"
        description="Unseals today so it can be counted again. The reopen is written to the day-close audit log."
        submitLabel="Reopen"
        pending={reopenDay.isPending}
        error={pinError}
        onSubmit={(pin) => {
          setPinError(null)
          reopenDay.mutate(pin)
        }}
      />

      <PinEntryModal
        open={forceOpen}
        onOpenChange={(next) => {
          if (forceClose.isPending) return
          setForceOpen(next)
          if (!next) setPinError(null)
        }}
        title="Force close without a count"
        description="Seals the day with nothing counted. Every variance stays unknown — use it only when the drawer cannot be counted at all."
        withReason
        reasonLabel="Why is this day being closed without a count?"
        reasonPlaceholder="Nobody was there to count it, shop closed unexpectedly…"
        submitLabel="Force close"
        submitVariant="destructive"
        pending={forceClose.isPending}
        error={pinError}
        onSubmit={(pin, reason) => {
          setPinError(null)
          forceClose.mutate({ pin, reason })
        }}
      />
    </div>
  )
}
