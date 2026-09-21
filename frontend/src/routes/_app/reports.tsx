import { createFileRoute } from '@tanstack/react-router'
import { PageHeader } from '@/components/shell/PageHeader'
import { ReportsTabs } from '@/components/reports/ReportsTabs'

export const Route = createFileRoute('/_app/reports')({ component: ReportsPage })

function ReportsPage() {
  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader title="Reports" description="One date range, seven views of it. Everything filters on the business date." />
      <ReportsTabs />
    </div>
  )
}
