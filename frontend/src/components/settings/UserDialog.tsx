import { useEffect } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Field } from '@/components/settings/Field'
import { toast } from '@/hooks/use-toast'
import { ROLES, roleLabel } from '@/lib/roles'
import { PASSWORD_MIN, USERNAME_HINT, userDialogSchema, type UserDialogValues } from '@/lib/userSchemas'
import type { User } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Present when editing. */
  user?: User
}

const EMPTY: UserDialogValues = { mode: 'create', username: '', email: '', password: '', first_name: '', last_name: '', role: 'counter', is_active: true }

function fromUser(u: User): UserDialogValues {
  return { mode: 'edit', username: u.username, email: u.email ?? '', password: '', first_name: u.first_name, last_name: u.last_name, role: u.role, is_active: u.is_active }
}

function apiMessage(err: unknown): { field?: 'username' | 'email' | 'root'; message: string } {
  if (err instanceof ApiClientError) {
    switch (err.code) {
      case 'username_taken':
        return { field: 'username', message: 'That username is already taken' }
      case 'email_taken':
        return { field: 'email', message: 'That email is already used by another account' }
      case 'cannot_modify_self':
        return { field: 'root', message: 'You cannot demote or deactivate your own account' }
      case 'last_admin':
        return { field: 'root', message: 'This is the only active admin. Add another admin first.' }
    }
    return { field: 'root', message: err.message }
  }
  return { field: 'root', message: 'Something went wrong' }
}

export function UserDialog({ open, onOpenChange, user }: Props) {
  const qc = useQueryClient()
  const editing = !!user
  const form = useForm<UserDialogValues>({ resolver: zodResolver(userDialogSchema), defaultValues: user ? fromUser(user) : EMPTY })
  useEffect(() => {
    if (open) form.reset(user ? fromUser(user) : EMPTY)
  }, [open, user, form])

  const mutation = useMutation({
    mutationFn: async (v: UserDialogValues) => {
      if (user) {
        return apiClient.updateUser(user.id, {
          email: v.email,
          first_name: v.first_name,
          last_name: v.last_name,
          role: v.role,
          is_active: v.is_active,
          ...(v.password ? { password: v.password } : {}),
        })
      }
      return apiClient.createUser({ username: v.username, email: v.email, password: v.password, first_name: v.first_name, last_name: v.last_name, role: v.role })
    },
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['users'] })
      toast({ title: user ? 'User updated' : 'User created', description: res.data ? `${res.data.first_name} ${res.data.last_name}`.trim() : undefined, variant: 'success' })
      onOpenChange(false)
    },
    onError: (err: unknown) => {
      const m = apiMessage(err)
      form.setError(m.field ?? 'root', { message: m.message })
    },
  })
  const { errors } = form.formState

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{editing ? `Edit ${user.username}` : 'New user'}</DialogTitle>
            <DialogDescription>{editing ? 'Changing the role, password or active flag signs the user out everywhere.' : 'Counter staff use the till and day close; admins see everything.'}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="First name" htmlFor="first_name" error={errors.first_name?.message}>
              <Input id="first_name" autoFocus {...form.register('first_name')} />
            </Field>
            <Field label="Last name" htmlFor="last_name" error={errors.last_name?.message}>
              <Input id="last_name" {...form.register('last_name')} />
            </Field>
            <Field label="Username" htmlFor="username" error={errors.username?.message} hint={editing ? 'Cannot be changed' : USERNAME_HINT}>
              <Input id="username" autoComplete="off" disabled={editing} {...form.register('username')} />
            </Field>
            <Field label="Email (optional)" htmlFor="email" error={errors.email?.message} hint="Needed for self-service password reset">
              <Input id="email" type="email" autoComplete="off" {...form.register('email')} />
            </Field>
            <Field label={editing ? 'New password (optional)' : 'Password'} htmlFor="password" error={errors.password?.message} hint={editing ? 'Leave blank to keep the current password' : `At least ${PASSWORD_MIN} characters`}>
              <Input id="password" type="password" autoComplete="new-password" {...form.register('password')} />
            </Field>
            <Field label="Role" htmlFor="role" error={errors.role?.message}>
              <Controller
                control={form.control}
                name="role"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="role">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {ROLES.map((r) => (
                        <SelectItem key={r} value={r}>
                          {roleLabel(r)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </Field>
            {editing && (
              <div className="flex items-center gap-3 sm:col-span-2">
                <Controller
                  control={form.control}
                  name="is_active"
                  render={({ field }) => <Switch id="is_active" checked={field.value} onCheckedChange={field.onChange} />}
                />
                <Label htmlFor="is_active">Active (can sign in)</Label>
              </div>
            )}
          </div>
          {errors.root && <p className="text-sm text-destructive">{errors.root.message}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : editing ? 'Save changes' : 'Create user'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
