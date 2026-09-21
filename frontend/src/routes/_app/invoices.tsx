import { createFileRoute } from '@tanstack/react-router'
import { PageHeader } from '@/components/shell/PageHeader'
import { InvoiceTable } from '@/components/invoices/InvoiceTable'

export const Route = createFileRoute('/_app/invoices')({ component: InvoicesPage })

function InvoicesPage() {
  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader title="Invoices" description="Every sale the shop has rung, filtered on the business date." />
      <InvoiceTable />
    </div>
  )
}
