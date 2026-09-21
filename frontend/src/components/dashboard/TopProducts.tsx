/**
 * The busiest products over the last 30 days (spec §3), not today — at nine
 * in the morning today's list is empty, and a card that is blank for the
 * first hours of every day tells the owner nothing (backend
 * handlers/reports.go: dashboardResponse.TopProducts). The card title says
 * "30 days" for exactly that reason; nothing here should be read as "today".
 */
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow, tableInCard } from '@/components/ui/table'
import { formatKgGrouped, formatMoney } from '@/lib/money'
import type { ProductRow } from '@/types'

interface Props {
  products: ProductRow[]
}

export function TopProducts({ products }: Props) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Top products, last 30 days</CardTitle>
        <CardDescription>The five biggest sellers by gross, last 30 days — not just today.</CardDescription>
      </CardHeader>
      <CardContent>
        {products.length === 0 ? (
          <p className="text-sm text-muted-foreground">Nothing has sold in the last 30 days.</p>
        ) : (
          <Table className={tableInCard}>
            <TableHeader>
              <TableRow>
                <TableHead>Product</TableHead>
                <TableHead className="text-right">Kg</TableHead>
                <TableHead className="text-right">Invoices</TableHead>
                <TableHead className="text-right">Gross</TableHead>
                <TableHead className="text-right">Share</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {products.map((p) => (
                <TableRow key={p.product_id ?? p.name}>
                  <TableCell className="font-semibold">{p.name}</TableCell>
                  <TableCell className="text-right tabular">{formatKgGrouped(p.kg)}</TableCell>
                  <TableCell className="text-right tabular">{p.invoices}</TableCell>
                  <TableCell className="text-right tabular">{formatMoney(p.gross)}</TableCell>
                  <TableCell className="text-right tabular">{p.share.toFixed(1)}%</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
