/**
 * The Day closes tab's drill-down: the same `ZReportView` the day-close
 * screen renders (`components/dayclose/ZReportView.tsx`), reused here in a
 * dialog rather than a second copy of the layout — the report row's own
 * `GET /day/:id/z` is the source of both. Works for any row status: an
 * open or reopened day shows the "X-read" state ZReportView already
 * handles, a closed one shows the sealed Z-report.
 *
 * No `actions` are passed (reopen / force-close stay on the day-close
 * screen, per ZReportView's own doc comment) — this dialog is read-only.
 */
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient from '@/api/client'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ZReportView } from '@/components/dayclose/ZReportView'
import { useSettings } from '@/components/settings/useSettings'
import { printZReport } from '@/lib/print/printInvoice'

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

interface Props {
  /** The business day to show, or null to keep the dialog closed. */
  dayId: string | null
  label: string
  onOpenChange: (open: boolean) => void
}

export function DayCloseDialog({ dayId, label, onOpenChange }: Props) {
  const { data: settings } = useSettings()
  const [printing, setPrinting] = useState(false)

  const z = useQuery({
    queryKey: ['reports', 'day-close-z', dayId ?? 'none'],
    queryFn: async () => {
      const res = await apiClient.getZReport(dayId as string)
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load the Z-report')
      return res.data
    },
    enabled: dayId !== null,
  })

  const onPrint = async () => {
    if (!settings || !z.data) return
    setPrinting(true)
    try {
      await printZReport(z.data, settings)
    } finally {
      setPrinting(false)
    }
  }

  return (
    <Dialog open={dayId !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{label}</DialogTitle>
        </DialogHeader>
        {z.isLoading && (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" /> Loading the Z-report…
          </p>
        )}
        {z.error && <p className="text-sm text-destructive">{errorMessage(z.error, 'Could not load the Z-report')}</p>}
        {z.data && <ZReportView z={z.data} settings={settings} onPrint={onPrint} printing={printing} />}
      </DialogContent>
    </Dialog>
  )
}
