import { createFileRoute } from '@tanstack/react-router'
import { ReportsTabs } from '@/components/reports/ReportsTabs'

export const Route = createFileRoute('/_app/reports')({ component: ReportsPage })

function ReportsPage() {
  return (
    <div className="space-y-4 p-4 md:p-6">
      <h1 className="text-2xl font-semibold">Reports</h1>
      <ReportsTabs />
    </div>
  )
}
