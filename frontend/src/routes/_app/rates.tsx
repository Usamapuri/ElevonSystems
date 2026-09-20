import { createFileRoute } from '@tanstack/react-router'
import { RateHistoryCard } from '@/components/rates/RateHistoryCard'
import { RatesTable } from '@/components/rates/RatesTable'

export const Route = createFileRoute('/_app/rates')({ component: RatesPage })

function RatesPage() {
  return (
    <div className="space-y-4 p-4 md:p-6">
      <h1 className="text-2xl font-semibold">Rates</h1>
      <RatesTable />
      <RateHistoryCard />
    </div>
  )
}
