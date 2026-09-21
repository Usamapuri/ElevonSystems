/** The top-of-till banner: why the Charge button is off, and the one screen
 * that fixes it. Renders nothing while the day is fine. */
import { Link } from '@tanstack/react-router'
import { AlertTriangle, CalendarClock, Info } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { DayGate } from './dayGate'

interface Props {
  gate: DayGate
  className?: string
}

export function DayGateBanner({ gate, className }: Props) {
  if (gate.kind === 'ok' || gate.kind === 'loading') return null

  if (gate.kind === 'late_sale') {
    return (
      <div
        className={cn(
          'flex items-start gap-3 rounded-lg border border-warning/40 bg-warning-soft px-4 py-3 text-sm',
          className,
        )}
      >
        <Info className="mt-0.5 h-5 w-5 shrink-0 text-warning-ink" />
        <div className="min-w-0">
          <p className="font-medium">Today is already closed.</p>
          <p className="text-muted-foreground">
            A sale now reopens {gate.date} for a late sale and writes an audit entry. Re-close the day when you are done.
          </p>
        </div>
      </div>
    )
  }

  const blocked =
    gate.kind === 'previous_day_open'
      ? {
          icon: <CalendarClock className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />,
          title: gate.date ? `${gate.date} is still open.` : 'An earlier business day is still open.',
          body: 'Close it before ringing today — an invoice cannot be dated into a day that is still counting.',
          action: 'Go to day close',
        }
      : {
          icon: <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />,
          title: 'No business day is open.',
          body: 'Declare the cash in the drawer to start the day before the first sale.',
          action: 'Open the day',
        }

  return (
    <div
      className={cn(
        'flex flex-col gap-3 rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between',
        className,
      )}
    >
      <div className="flex items-start gap-3">
        {blocked.icon}
        <div className="min-w-0">
          <p className="font-medium">{blocked.title}</p>
          <p className="text-muted-foreground">{blocked.body}</p>
        </div>
      </div>
      <Button asChild size="sm" className="shrink-0">
        <Link to="/day-close">{blocked.action}</Link>
      </Button>
    </div>
  )
}
