import { createFileRoute } from '@tanstack/react-router'
import { PageHeader } from '@/components/shell/PageHeader'
import { RateHistoryCard } from '@/components/rates/RateHistoryCard'
import { RatesTable } from '@/components/rates/RatesTable'

export const Route = createFileRoute('/_app/rates')({ component: RatesPage })

function RatesPage() {
  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader title="Rates" description="What each product sells for today, and every change behind it." />
      <RatesTable />
      <RateHistoryCard />
    </div>
  )
}
