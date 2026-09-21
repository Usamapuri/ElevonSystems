import { createFileRoute, Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { ArrowLeft, CheckCircle2, Loader2, Mail } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { AuthShell } from '@/components/shell/AuthShell'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export const Route = createFileRoute('/forgot-password')({ component: ForgotPasswordPage })

// The confirmation reads the same whether or not the email exists — the
// server guarantees that too, so nothing here may leak membership.
function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [submitted, setSubmitted] = useState(false)
  const [error, setError] = useState('')

  const send = useMutation({
    mutationFn: (value: string) => apiClient.forgotPassword(value),
    onSuccess: () => setSubmitted(true),
    onError: (err: unknown) => {
      if (err instanceof ApiClientError && err.code === 'rate_limited') setError(err.message)
      else if (err instanceof ApiClientError && err.isNetworkError) setError('Cannot reach the server. Check the connection and try again.')
      else setSubmitted(true)
    },
  })

  if (submitted) {
    return (
      <AuthShell>
        <div className="space-y-4">
          <CheckCircle2 className="h-10 w-10 text-success-ink" />
          <h1 className="text-2xl font-semibold">Check your email</h1>
          <p className="text-sm text-muted-foreground">
            If <span className="font-medium text-foreground">{email.trim()}</span> belongs to a staff account, a reset link is on its way. It is valid for 1 hour.
          </p>
          <p className="text-sm text-muted-foreground">No email? Check spam, or ask your admin to reset the password from Settings → Users.</p>
          <Link to="/login" className="inline-flex items-center gap-1 text-sm font-semibold underline underline-offset-2 hover:text-primary-hover">
            <ArrowLeft className="h-4 w-4" /> Back to sign in
          </Link>
        </div>
      </AuthShell>
    )
  }

  return (
    <AuthShell>
      <form
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault()
          setError('')
          if (!email.trim()) {
            setError('Enter the email on your account.')
            return
          }
          send.mutate(email.trim())
        }}
      >
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">Forgot your password?</h1>
          <p className="text-sm text-muted-foreground">Enter the email on your staff account and we will send a reset link.</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="email">Email</Label>
          <div className="relative">
            <Mail className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input id="email" type="email" className="pl-9" autoComplete="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />
          </div>
        </div>
        {error && <div className="rounded-md border border-destructive/30 bg-destructive-soft px-3 py-2 text-sm font-semibold text-destructive-ink">{error}</div>}
        <Button type="submit" className="w-full" disabled={send.isPending}>
          {send.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Send reset link'}
        </Button>
        <Link to="/login" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-4 w-4" /> Back to sign in
        </Link>
      </form>
    </AuthShell>
  )
}
