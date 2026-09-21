/**
 * The one header row every screen wears: the page title on the left, the
 * business-day status on the right.
 *
 * The day gate is this app's identity — nothing can be sold into a day that
 * is not open, and a day left open overnight blocks tomorrow's first sale
 * (spec §6.7). That fact used to be visible only on the till and on Day
 * close, which meant the owner could sit on Reports all afternoon without
 * knowing the counter had never opened the drawer. So the pill lives in the
 * header on every screen.
 *
 * It reads the SAME query key the till and Day close already use
 * (`['day','current']`), so mounting this costs no extra round trip on those
 * screens and exactly one on the others. The state is derived through
 * `evaluateDayGate`, the same pure function the till's banner uses, so the
 * pill and the banner can never disagree about whether today is open.
 *
 * The pill is deliberately not a link: this is a status indicator, and the
 * screen that fixes it is one tap away in the rail.
 */
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import apiClient from '@/api/client'
import { useSettings } from '@/components/settings/useSettings'
import { evaluateDayGate } from '@/components/pos/dayGate'
import { formatBusinessDate } from '@/lib/print/format'
import { cn } from '@/lib/utils'

/** Identical to the till's and Day close's key, on purpose — one fetch. */
const DAY_CURRENT_KEY = ['day', 'current'] as const

interface Props {
  title: string
  /** One line under the title. Kept short; the page says the rest. */
  description?: string
  /** Page-level actions, shown between the title and the day pill. */
  actions?: ReactNode
}

type PillTone = 'open' | 'closed' | 'attention'

const TONE_CLASS: Record<PillTone, string> = {
  open: 'border-success/30 bg-success-soft text-success-ink',
  closed: 'border-border bg-muted text-muted-foreground',
  attention: 'border-warning/30 bg-warning-soft text-warning-ink',
}

export function PageHeader({ title, description, actions }: Props) {
  const { data: settings } = useSettings()

  const day = useQuery({
    queryKey: DAY_CURRENT_KEY,
    queryFn: async () => {
      const res = await apiClient.getDayCurrent()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the business day')
      return res.data
    },
    refetchOnWindowFocus: true,
  })

  const pill = dayPill(day.data?.day ?? null, settings?.day_boundary_hour, day.isLoading || !settings, day.isError)

  return (
    <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
      <div className="min-w-0">
        <h1 className="truncate text-2xl font-bold tracking-tight">{title}</h1>
        {description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {actions}
        {pill && (
          <span
            className={cn(
              'inline-flex h-8 items-center rounded-md border px-2.5 text-xs font-bold',
              TONE_CLASS[pill.tone],
            )}
          >
            {pill.label}
          </span>
        )}
      </div>
    </header>
  )
}

/**
 * Four words for four states, in the cashier's language rather than the
 * table's: `status` alone cannot tell "closed, and that is fine, it is
 * tonight" from "an earlier day never got closed", which is the one that
 * stops the next sale.
 */
function dayPill(
  day: Parameters<typeof evaluateDayGate>[0],
  boundaryHour: number | undefined,
  loading: boolean,
  errored: boolean,
): { label: string; tone: PillTone } | null {
  if (loading) return null
  if (errored) return { label: 'Day unknown', tone: 'attention' }

  const gate = evaluateDayGate(day, boundaryHour ?? 0, new Date())
  switch (gate.kind) {
    case 'ok':
      return { label: 'Day open', tone: 'open' }
    case 'late_sale':
      return { label: 'Day closed', tone: 'closed' }
    case 'previous_day_open':
      return { label: `${formatBusinessDate(gate.date)} still open`, tone: 'attention' }
    case 'day_not_open':
      return { label: 'Day not opened', tone: 'attention' }
    default:
      return null
  }
}
