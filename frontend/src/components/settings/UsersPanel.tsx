import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { KeyRound, Pencil, Search, UserPlus } from 'lucide-react'
import apiClient from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { SetPinDialog } from '@/components/settings/SetPinDialog'
import { UserDialog } from '@/components/settings/UserDialog'
import { roleLabel } from '@/lib/roles'
import type { User } from '@/types'

const PER_PAGE = 50

// Pinned to Asia/Karachi rather than the browser's zone: an owner checking
// this screen from outside the shop (or a machine whose clock is set wrong)
// must see the same last-sign-in time the till itself would show.
function formatLastLogin(iso: string | null): string {
  if (!iso) return 'Never'
  return new Date(iso).toLocaleString('en-PK', { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Asia/Karachi' })
}

export function UsersPanel() {
  const [search, setSearch] = useState('')
  const [debounced, setDebounced] = useState('')
  const [page, setPage] = useState(1)
  const [createOpen, setCreateOpen] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [pinUser, setPinUser] = useState<User | null>(null)

  useEffect(() => {
    const t = setTimeout(() => {
      setDebounced(search.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(t)
  }, [search])

  const { data, isLoading, error } = useQuery({
    queryKey: ['users', { search: debounced, page }],
    queryFn: () => apiClient.getUsers({ search: debounced || undefined, page, per_page: PER_PAGE }),
  })
  const users = data?.data ?? []
  const totalPages = data?.meta.total_pages ?? 1

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>Users</CardTitle>
          <CardDescription>Staff accounts. Deactivate instead of deleting so past invoices keep their cashier.</CardDescription>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <UserPlus className="mr-2 h-4 w-4" /> New user
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="relative max-w-sm">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-9" placeholder="Search name, username or email" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        {error && <p className="text-sm text-red-600">{error instanceof Error ? error.message : 'Could not load users'}</p>}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Username</TableHead>
              <TableHead className="hidden md:table-cell">Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="hidden md:table-cell">Last sign-in</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground">Loading…</TableCell>
              </TableRow>
            )}
            {!isLoading && users.length === 0 && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground">No users match.</TableCell>
              </TableRow>
            )}
            {users.map((u) => (
              <TableRow key={u.id} className={u.is_active ? '' : 'opacity-60'}>
                <TableCell className="font-medium">{`${u.first_name} ${u.last_name}`.trim()}</TableCell>
                <TableCell>{u.username}</TableCell>
                <TableCell className="hidden md:table-cell">{u.email ?? <span className="text-muted-foreground">—</span>}</TableCell>
                <TableCell>
                  <Badge variant={u.role === 'admin' ? 'default' : 'secondary'}>{roleLabel(u.role)}</Badge>
                  {u.role === 'admin' && u.has_pin && <Badge variant="outline" className="ml-1">PIN</Badge>}
                </TableCell>
                <TableCell>{u.is_active ? 'Active' : 'Inactive'}</TableCell>
                <TableCell className="hidden md:table-cell text-muted-foreground">{formatLastLogin(u.last_login_at)}</TableCell>
                <TableCell className="text-right">
                  <div className="inline-flex gap-1">
                    {u.role === 'admin' && u.is_active && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setPinUser(u)}
                        title={u.has_pin ? 'Change PIN' : 'Set PIN'}
                        aria-label={`${u.has_pin ? 'Change PIN' : 'Set PIN'} for ${u.username}`}
                      >
                        <KeyRound className="h-4 w-4" />
                      </Button>
                    )}
                    <Button variant="ghost" size="sm" onClick={() => setEditing(u)} title="Edit" aria-label={`Edit ${u.username}`}>
                      <Pencil className="h-4 w-4" />
                    </Button>
                  </div>
                </TableCell>
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

      <UserDialog open={createOpen} onOpenChange={setCreateOpen} />
      <UserDialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} user={editing ?? undefined} />
      <SetPinDialog open={!!pinUser} onOpenChange={(o) => !o && setPinUser(null)} user={pinUser} />
    </Card>
  )
}
