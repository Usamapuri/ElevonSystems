import { createFileRoute } from '@tanstack/react-router'
import { InvoiceTable } from '@/components/invoices/InvoiceTable'

export const Route = createFileRoute('/_app/invoices')({ component: InvoicesPage })

function InvoicesPage() {
  return (
    <div className="space-y-4 p-4 md:p-6">
      <h1 className="text-2xl font-semibold">Invoices</h1>
      <InvoiceTable />
    </div>
  )
}
