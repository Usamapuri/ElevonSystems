/**
 * CSV / Excel export for one report tab (spec §6.8). Streams the file
 * through `apiClient.downloadReport`, which saves it under the filename the
 * server names in `Content-Disposition` — this component never invents a
 * filename itself. The Daily tab additionally offers the "Period pack": one
 * workbook with Summary, Daily, Products, Tax, Cashiers and Receivables
 * sheets (`format=xlsx&pack=1`), which is why `withPeriodPack` exists at
 * all rather than being every tab's business.
 */
import { useState } from 'react'
import { Download, FileSpreadsheet, Layers, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import apiClient from '@/api/client'
import { toast } from '@/hooks/use-toast'
import { formatBusinessDate } from '@/lib/print/format'
import type { ReportName } from '@/types'

type Job = 'csv' | 'xlsx' | 'pack' | null

interface Props {
  report: ReportName
  reportLabel: string
  from: string
  to: string
  /** Only the Daily tab passes this — the period pack bundles five other
   * reports behind the Daily one, not the tab currently open. */
  withPeriodPack?: boolean
  /** Set while the range is not ready or is inverted — there is nothing a
   * download could answer for either. */
  disabled?: boolean
}

export function ExportButton({ report, reportLabel, from, to, withPeriodPack = false, disabled = false }: Props) {
  const [busy, setBusy] = useState<Job>(null)

  const run = async (job: Exclude<Job, null>, format: 'csv' | 'xlsx', pack: boolean, label: string) => {
    setBusy(job)
    try {
      await apiClient.downloadReport(report, { from, to, format, pack })
      toast({
        title: `${label} downloaded`,
        description: `${reportLabel} · ${formatBusinessDate(from)} – ${formatBusinessDate(to)}`,
        variant: 'success',
      })
    } catch (e) {
      toast({
        title: 'Export failed',
        description: e instanceof Error ? e.message : 'Unknown error',
        variant: 'destructive',
      })
    } finally {
      setBusy(null)
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" disabled={disabled || busy !== null} className="gap-2">
          {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
          Export
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="text-xs">Export {reportLabel}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => run('csv', 'csv', false, 'CSV')}>
          <FileSpreadsheet className="mr-2 h-4 w-4" /> CSV
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => run('xlsx', 'xlsx', false, 'Excel')}>
          <FileSpreadsheet className="mr-2 h-4 w-4" /> Excel (.xlsx)
        </DropdownMenuItem>
        {withPeriodPack && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => run('pack', 'xlsx', true, 'Period pack')}>
              <Layers className="mr-2 h-4 w-4" /> Period pack (.xlsx)
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
