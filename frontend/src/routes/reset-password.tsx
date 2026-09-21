import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { AlertCircle, ArrowLeft, Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { AuthShell } from '@/components/shell/AuthShell'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from '@/hooks/use-toast'
import { newPasswordSchema, PASSWORD_MIN, type NewPasswordValues } from '@/lib/userSchemas'

export const Route = createFileRoute('/reset-password')({ component: ResetPasswordPage })

function ResetPasswordPage() {
  const navigate = useNavigate()
  // The token is opaque and validated server-side; no search schema needed.
  const token = useMemo(() => new URLSearchParams(window.location.search).get('token') ?? '', [])
  const form = useForm<NewPasswordValues>({ resolver: zodResolver(newPasswordSchema), defaultValues: { new_password: '', confirm: '' } })

  const reset = useMutation({
    mutationFn: (v: NewPasswordValues) => apiClient.resetPassword(token, v.new_password),
    onSuccess: () => {
      toast({ title: 'Password updated', description: 'Sign in with your new password.', variant: 'success' })
      navigate({ to: '/login' })
    },
    onError: (err: unknown) => {
      const message =
        err instanceof ApiClientError && err.code === 'invalid_or_expired_token'
          ? 'This reset link is invalid or has expired. Request a new one.'
          : err instanceof Error
            ? err.message
            : 'Could not reset the password'
      form.setError('root', { message })
    },
  })

  if (!token) {
    return (
      <AuthShell>
        <div className="space-y-4">
          <AlertCircle className="h-10 w-10 text-destructive" />
          <h1 className="text-2xl font-semibold">Missing reset link</h1>
          <p className="text-sm text-muted-foreground">Open the full link from the reset email; some mail apps cut long links.</p>
          <Link to="/forgot-password">
            <Button>Request a new link</Button>
          </Link>
        </div>
      </AuthShell>
    )
  }

  const { errors } = form.formState
  return (
    <AuthShell>
      <form className="space-y-5" onSubmit={form.handleSubmit((v) => reset.mutate(v))}>
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">Set a new password</h1>
          <p className="text-sm text-muted-foreground">At least {PASSWORD_MIN} characters.</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="new_password">New password</Label>
          <Input id="new_password" type="password" autoComplete="new-password" autoFocus {...form.register('new_password')} />
          {errors.new_password && <p className="text-sm text-destructive">{errors.new_password.message}</p>}
        </div>
        <div className="space-y-2">
          <Label htmlFor="confirm">Confirm new password</Label>
          <Input id="confirm" type="password" autoComplete="new-password" {...form.register('confirm')} />
          {errors.confirm && <p className="text-sm text-destructive">{errors.confirm.message}</p>}
        </div>
        {errors.root && <div className="rounded-md border border-destructive/30 bg-destructive-soft px-3 py-2 text-sm font-semibold text-destructive-ink">{errors.root.message}</div>}
        <Button type="submit" className="w-full" disabled={reset.isPending}>
          {reset.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Update password'}
        </Button>
        <Link to="/login" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> Back to sign in
        </Link>
      </form>
    </AuthShell>
  )
}
