/**
 * The Reports screen (spec §6.8, §3 `/reports` row): one date-range filter
 * shared by seven tabs — Daily · Products · Tax · Cashiers · Hourly ·
 * Receivables · Day closes — each backed by `GET /admin/reports/:name` for
 * the same window. Radix `Tabs` only mounts the active panel's content, so
 * switching tabs is what fires that tab's first query, not a page load that
 * runs all seven at once.
 */
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { DateRangeFilter } from './DateRangeFilter'
import { useReportRange } from './useReportRange'
import {
  CashiersPanel, DailyPanel, DayClosesPanel, HourlyPanel, ProductsPanel, ReceivablesPanel, TaxPanel,
} from './ReportPanels'
import type { ReportName } from '@/types'

const TABS: { name: ReportName; label: string }[] = [
  { name: 'daily', label: 'Daily' },
  { name: 'products', label: 'Products' },
  { name: 'tax', label: 'Tax' },
  { name: 'cashiers', label: 'Cashiers' },
  { name: 'hourly', label: 'Hourly' },
  { name: 'receivables', label: 'Receivables' },
  { name: 'day-closes', label: 'Day closes' },
]

export function ReportsTabs() {
  const range = useReportRange()
  const enabled = range.ready && !range.inverted
  const panelProps = { from: range.from, to: range.to, params: range.params, enabled }

  return (
    <div className="space-y-4">
      <DateRangeFilter range={range} />

      <Tabs defaultValue="daily">
        <TabsList className="flex-wrap">
          {TABS.map((t) => (
            <TabsTrigger key={t.name} value={t.name}>
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="daily">
          <DailyPanel {...panelProps} />
        </TabsContent>
        <TabsContent value="products">
          <ProductsPanel {...panelProps} />
        </TabsContent>
        <TabsContent value="tax">
          <TaxPanel {...panelProps} />
        </TabsContent>
        <TabsContent value="cashiers">
          <CashiersPanel {...panelProps} />
        </TabsContent>
        <TabsContent value="hourly">
          <HourlyPanel {...panelProps} />
        </TabsContent>
        <TabsContent value="receivables">
          <ReceivablesPanel {...panelProps} />
        </TabsContent>
        <TabsContent value="day-closes">
          <DayClosesPanel {...panelProps} />
        </TabsContent>
      </Tabs>
    </div>
  )
}
