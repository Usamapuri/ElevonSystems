import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from '@/hooks/use-toast'
import { changePasswordSchema, PASSWORD_MIN, type ChangePasswordValues } from '@/lib/userSchemas'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ChangePasswordDialog({ open, onOpenChange }: Props) {
  const form = useForm<ChangePasswordValues>({
    resolver: zodResolver(changePasswordSchema),
    defaultValues: { current_password: '', new_password: '', confirm: '' },
  })
  const change = useMutation({
    mutationFn: (v: ChangePasswordValues) => apiClient.changePassword(v.current_password, v.new_password),
    onSuccess: () => {
      // The server revoked every session, this one included.
      toast({ title: 'Password updated', description: 'Sign in again with your new password.', variant: 'success' })
      apiClient.clearAuth()
      window.location.href = '/login'
    },
    onError: (err: unknown) => {
      if (err instanceof ApiClientError && err.code === 'invalid_current_password') form.setError('current_password', { message: 'Current password is incorrect' })
      else form.setError('root', { message: err instanceof Error ? err.message : 'Could not change the password' })
    },
  })
  const { errors } = form.formState
  const close = () => {
    form.reset()
    onOpenChange(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) close()
        else onOpenChange(o)
      }}
    >
      <DialogContent className="sm:max-w-md">
        <form onSubmit={form.handleSubmit((v) => change.mutate(v))} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Change password</DialogTitle>
            <DialogDescription>You will be signed out everywhere and asked to sign in again.</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="current_password">Current password</Label>
            <Input id="current_password" type="password" autoComplete="current-password" {...form.register('current_password')} />
            {errors.current_password && <p className="text-sm text-destructive">{errors.current_password.message}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="new_password">New password (at least {PASSWORD_MIN} characters)</Label>
            <Input id="new_password" type="password" autoComplete="new-password" {...form.register('new_password')} />
            {errors.new_password && <p className="text-sm text-destructive">{errors.new_password.message}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="confirm">Confirm new password</Label>
            <Input id="confirm" type="password" autoComplete="new-password" {...form.register('confirm')} />
            {errors.confirm && <p className="text-sm text-destructive">{errors.confirm.message}</p>}
          </div>
          {errors.root && <p className="text-sm text-destructive">{errors.root.message}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={close} disabled={change.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={change.isPending}>
              {change.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Update password'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
