/**
 * Revenue and kg sold, per day, over a 7d/30d window the owner can toggle
 * (spec §3, Task P4). Net revenue and kg sold live on two different scales
 * (rupees vs kilograms), so this is deliberately a two-axis chart — the
 * alternative is two separate charts the owner has to line up by eye against
 * the same x-axis, which is worse for the one question this card answers:
 * "did the kg sold track the money, or did the rate carry the day?"
 *
 * Both series come from the same `reports.Daily` rows the dashboard's 30-day
 * series already padded (backend: fillDailySeries) — the 7-day view is just
 * the tail of the 30-day one, so the two toggle states can never disagree
 * about a shared date.
 */
import { useState } from 'react'
import {
  Bar,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatKgGrouped, formatKgTick, formatMoney } from '@/lib/money'
import { cn } from '@/lib/utils'
import { dayMonthLabel } from './kpis'
import type { DailyRow } from '@/types'

interface Props {
  series7d: DailyRow[]
  series30d: DailyRow[]
}

type RangeToggle = '7d' | '30d'

// The chrome's own two colours: navy bars for money, safety orange for the
// kg line. Navy against orange is the canonical colour-vision-safe pair —
// the two series stay tellable apart under every common CVD simulation — and
// the mark itself differs (bar versus line), so hue is never the only cue.
// Both are read from the theme so the chart follows light, dark and
// high-contrast without a second palette to maintain.
const REVENUE_COLOR = 'hsl(var(--chart-bar))'
const KG_COLOR = 'hsl(var(--chart-line))'

interface ChartRow {
  label: string
  net: number
  kg: number
}

function toChartRow(row: DailyRow): ChartRow {
  return { label: dayMonthLabel(row.label), net: row.net, kg: row.kg_sold }
}

function tooltipValue(value: unknown, name: unknown): [string, string] {
  const n = typeof value === 'number' ? value : Number(Array.isArray(value) ? value[0] : value)
  const label = String(name ?? '')
  if (label === 'Kg sold') return [formatKgGrouped(n), label]
  return [formatMoney(n), label]
}

export function TrendChart({ series7d, series30d }: Props) {
  const [range, setRange] = useState<RangeToggle>('7d')
  const rows = (range === '7d' ? series7d : series30d).map(toChartRow)

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">Revenue and kg sold</CardTitle>
        <div className="flex gap-1 rounded-md border border-border bg-secondary p-1">
          {(['7d', '30d'] as const).map((r) => (
            <Button
              key={r}
              type="button"
              size="sm"
              variant="ghost"
              className={cn(
                'h-8 rounded-sm px-3 text-xs',
                range === r ? 'bg-card text-foreground hover:bg-card' : 'text-muted-foreground',
              )}
              aria-pressed={range === r}
              onClick={() => setRange(r)}
            >
              {r === '7d' ? '7 days' : '30 days'}
            </Button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">No activity in this window yet.</p>
        ) : (
          <div className="h-72 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <ComposedChart data={rows} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="hsl(var(--border))" />
                <XAxis
                  dataKey="label"
                  tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
                  stroke="hsl(var(--border))"
                />
                <YAxis
                  yAxisId="net"
                  tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
                  stroke="hsl(var(--border))"
                  tickFormatter={(v: number) => formatMoney(v)}
                  width={96}
                />
                <YAxis
                  yAxisId="kg"
                  orientation="right"
                  tick={{ fontSize: 11, fill: 'hsl(var(--muted-foreground))' }}
                  stroke="hsl(var(--border))"
                  tickFormatter={formatKgTick}
                  width={72}
                />
                <Tooltip
                  formatter={tooltipValue}
                  contentStyle={{
                    background: 'hsl(var(--popover))',
                    color: 'hsl(var(--popover-foreground))',
                    border: '1px solid hsl(var(--border))',
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                <Bar yAxisId="net" dataKey="net" name="Net revenue" fill={REVENUE_COLOR} radius={[4, 4, 0, 0]} />
                <Line
                  yAxisId="kg"
                  dataKey="kg"
                  name="Kg sold"
                  stroke={KG_COLOR}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 4 }}
                />
              </ComposedChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
