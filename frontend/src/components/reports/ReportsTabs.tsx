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
import { FiscalQueueTab } from './FiscalQueueTab'
import { useSettings } from '@/components/settings/useSettings'
import type { ReportName } from '@/types'

const TABS: { name: ReportName | 'fbr-queue'; label: string }[] = [
  { name: 'daily', label: 'Daily' },
  { name: 'products', label: 'Products' },
  { name: 'tax', label: 'Tax' },
  { name: 'cashiers', label: 'Cashiers' },
  { name: 'hourly', label: 'Hourly' },
  { name: 'receivables', label: 'Receivables' },
  { name: 'day-closes', label: 'Day closes' },
  { name: 'fbr-queue', label: 'FBR queue' },
]

export function ReportsTabs() {
  const range = useReportRange()
  const enabled = range.ready && !range.inverted
  const panelProps = { from: range.from, to: range.to, params: range.params, enabled }
  // Day closes are automatic, uncounted seals with the switch off (spec
  // §6.7 addendum): nothing in that tab would mean anything, so it goes.
  const { data: settings } = useSettings()
  const tabs = settings?.day_close_enabled === false ? TABS.filter((t) => t.name !== 'day-closes') : TABS

  return (
    <div className="space-y-4">
      <DateRangeFilter range={range} />

      <Tabs defaultValue="daily">
        <TabsList className="h-auto flex-wrap justify-start">
          {tabs.map((t) => (
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
        <TabsContent value="fbr-queue">
          <FiscalQueueTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
