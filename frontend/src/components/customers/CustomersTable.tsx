import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Pencil, Plus, Search } from 'lucide-react'
import apiClient from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { CustomerDetail } from '@/components/customers/CustomerDetail'
import { CustomerDialog } from '@/components/customers/CustomerDialog'
import { formatMoney } from '@/lib/money'
import type { Customer, Role } from '@/types'

const PER_PAGE = 50

interface Props {
  /** Only admin sees "New customer" and the per-row Edit button — the API
   * gate (admin group in routes.go) is the real guard; this only hides
   * controls a counter's request would be rejected for anyway. */
  role: Role
}

export function CustomersTable({ role }: Props) {
  const canEdit = role === 'admin'
  const [search, setSearch] = useState('')
  const [debounced, setDebounced] = useState('')
  const [page, setPage] = useState(1)
  const [createOpen, setCreateOpen] = useState(false)
  const [editing, setEditing] = useState<Customer | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  useEffect(() => {
    const t = setTimeout(() => {
      setDebounced(search.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(t)
  }, [search])

  const { data, isLoading, error } = useQuery({
    queryKey: ['customers', { search: debounced, page }],
    queryFn: () => apiClient.getCustomers({ search: debounced || undefined, page, per_page: PER_PAGE }),
  })
  const customers = data?.data ?? []
  const totalPages = data?.meta.total_pages ?? 1

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>Customers</CardTitle>
          <CardDescription>Balance is the account total: invoices on credit minus payments received.</CardDescription>
        </div>
        {canEdit && (
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" /> New customer
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="relative max-w-sm">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-9" placeholder="Search name or phone" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        {error && <p className="text-sm text-red-600">{error instanceof Error ? error.message : 'Could not load customers'}</p>}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="hidden md:table-cell">Phone</TableHead>
              <TableHead className="text-right">Balance</TableHead>
              <TableHead className="hidden md:table-cell">Credit</TableHead>
              <TableHead>Status</TableHead>
              {canEdit && <TableHead className="text-right">Actions</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={canEdit ? 6 : 5} className="text-center text-muted-foreground">Loading…</TableCell>
              </TableRow>
            )}
            {!isLoading && customers.length === 0 && (
              <TableRow>
                <TableCell colSpan={canEdit ? 6 : 5} className="text-center text-muted-foreground">No customers match.</TableCell>
              </TableRow>
            )}
            {customers.map((cust) => (
              <TableRow key={cust.id} className={`cursor-pointer ${cust.is_active ? '' : 'opacity-60'}`} onClick={() => setSelectedId(cust.id)}>
                <TableCell className="font-medium">{cust.name}</TableCell>
                <TableCell className="hidden md:table-cell">{cust.phone ?? <span className="text-muted-foreground">—</span>}</TableCell>
                <TableCell className={`text-right ${cust.balance > 0 ? 'text-amber-600 font-medium' : ''}`}>{formatMoney(cust.balance)}</TableCell>
                <TableCell className="hidden md:table-cell">
                  {cust.credit_allowed ? (
                    <Badge variant="outline">{cust.credit_limit === null ? 'No limit' : formatMoney(cust.credit_limit)}</Badge>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </TableCell>
                <TableCell>{cust.is_active ? 'Active' : 'Inactive'}</TableCell>
                {canEdit && (
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      title="Edit"
                      aria-label={`Edit ${cust.name}`}
                      onClick={(e) => {
                        e.stopPropagation()
                        setEditing(cust)
                      }}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {totalPages > 1 && (
          <div className="flex items-center justify-end gap-2 text-sm">
            <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Previous</Button>
            <span className="text-muted-foreground">Page {page} of {totalPages}</span>
            <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>Next</Button>
          </div>
        )}
      </CardContent>

      {canEdit && <CustomerDialog open={createOpen} onOpenChange={setCreateOpen} />}
      {canEdit && <CustomerDialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} customer={editing ?? undefined} />}
      <CustomerDetail customerId={selectedId} onOpenChange={(o) => !o && setSelectedId(null)} />
    </Card>
  )
}
