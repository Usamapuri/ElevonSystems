import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { PageHeader } from '@/components/shell/PageHeader'
import { BatchList } from '@/components/fiscal/BatchList'
import { FileWithFbr } from '@/components/fiscal/FileWithFbr'
import { Badge } from '@/components/ui/badge'
import type { FiscalBatch } from '@/types'

export const Route = createFileRoute('/_app/fbr')({ component: FbrPage })

/** What the last filing came to, shown above the list until the next one. */
function Outcome({ batch }: { batch: FiscalBatch }) {
  if (batch.status === 'synced') {
    return (
      <div className="rounded-lg border border-success/40 bg-success/10 p-3 text-sm">
        <Badge variant="success">Filed</Badge> FBR invoice <span className="tabular font-semibold">{batch.fbr_invoice_number}</span> covers{' '}
        {batch.invoice_count} {batch.invoice_count === 1 ? 'sale' : 'sales'}.
      </div>
    )
  }
  if (batch.status === 'failed') {
    return (
      <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm">
        <Badge variant="destructive">Rejected</Badge> FBR refused it ({batch.last_error_code ?? 'unknown'}
        {batch.last_error ? `: ${batch.last_error}` : ''}). The invoices are back in the list.
      </div>
    )
  }
  return (
    <div className="rounded-lg border border-border bg-card p-3 text-sm">
      <Badge variant="warning">Filing…</Badge> FBR is not answering — it will file automatically.
    </div>
  )
}

function FbrPage() {
  const [last, setLast] = useState<FiscalBatch | null>(null)
  return (
    <div className="space-y-6 p-4 md:p-6">
      <PageHeader title="File with FBR" description="Tick the sales to file together as one combined FBR invoice. Every figure is what the till recorded." />
      {last && <Outcome batch={last} />}
      <FileWithFbr onFiled={setLast} />
      <section className="space-y-2">
        <h2 className="text-base font-bold">Combined invoices — last 30 days</h2>
        <BatchList />
      </section>
    </div>
  )
}
