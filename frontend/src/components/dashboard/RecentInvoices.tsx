/**
 * The last few sales across the whole shop, voids included: a void a cashier
 * just did is exactly what the owner opened this screen to see (backend
 * handlers/reports.go: recentInvoices). Read-only — void and reprint live on
 * the till's own recent-sales rail (components/pos/RecentInvoices.tsx) and
 * on the invoice browser; this card is a compact summary, not a second place
 * to action a sale, and the lean `DashboardInvoice` shape it is fed has no
 * `lines` to reprint from anyway.
 */
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatMoney } from '@/lib/money'
import { formatTimePK, paymentMethodLabel } from '@/lib/print/format'
import { cn } from '@/lib/utils'
import type { DashboardInvoice } from '@/types'

interface Props {
  invoices: DashboardInvoice[]
}

export function RecentInvoices({ invoices }: Props) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">Recent invoices</CardTitle>
        <CardDescription>The last few sales, voids included.</CardDescription>
      </CardHeader>
      <CardContent>
        {invoices.length === 0 ? (
          <p className="text-sm text-muted-foreground">No sales yet.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Time</TableHead>
                <TableHead>Number</TableHead>
                <TableHead>Customer</TableHead>
                <TableHead className="hidden lg:table-cell">Cashier</TableHead>
                <TableHead>Tender</TableHead>
                <TableHead className="text-right">Total</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {invoices.map((inv) => {
                const voided = inv.status === 'voided'
                return (
                  <TableRow key={inv.id} className={cn(voided && 'opacity-60')}>
                    <TableCell className="whitespace-nowrap text-muted-foreground tabular-nums">
                      {formatTimePK(inv.created_at)}
                    </TableCell>
                    <TableCell className="font-medium tabular-nums">{inv.invoice_number}</TableCell>
                    <TableCell>{inv.customer_name ?? <span className="text-muted-foreground">Walk-in</span>}</TableCell>
                    <TableCell className="hidden lg:table-cell text-muted-foreground">{inv.cashier_name}</TableCell>
                    <TableCell>{paymentMethodLabel(inv.payment_method)}</TableCell>
                    <TableCell className="text-right font-medium tabular-nums">{formatMoney(inv.total_payable)}</TableCell>
                    <TableCell>
                      {voided ? <Badge variant="destructive">Voided</Badge> : <Badge variant="secondary">Completed</Badge>}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
