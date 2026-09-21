import { createFileRoute, Link, redirect, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Eye, EyeOff, Loader2, Lock, Scale, User as UserIcon, FileCheck2, BookUser } from 'lucide-react'
import { BrandMark } from '@/components/shell/BrandMark'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import apiClient, { ApiClientError } from '@/api/client'
import { defaultPath, isRole } from '@/lib/roles'
import type { LoginRequest } from '@/types'

export const Route = createFileRoute('/login')({
  // Already signed in: skip the form. Done in beforeLoad, never via <Navigate>
  // in render (see _app.tsx).
  beforeLoad: () => {
    const stored = apiClient.getStoredUser()
    if (apiClient.isAuthenticated() && stored && isRole(stored.role)) {
      throw redirect({ to: defaultPath(stored.role), replace: true })
    }
  },
  component: LoginPage,
})

const FEATURES = [
  { icon: Scale, text: 'Weigh it, price it, print it — kg, tonnes or a rupee amount.' },
  { icon: BookUser, text: 'Credit customers with a running ledger and receipts.' },
  { icon: FileCheck2, text: 'FBR Digital Invoicing the moment it is configured.' },
]

function LoginPage() {
  const router = useRouter()
  const [form, setForm] = useState<LoginRequest>({ username: '', password: '' })
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')

  const login = useMutation({
    mutationFn: (req: LoginRequest) => apiClient.login(req),
    onSuccess: (res) => {
      if (res.success && res.data) {
        apiClient.setAuth(res.data.token, res.data.user)
        router.navigate({ to: defaultPath(res.data.user.role) })
      } else {
        setError(res.message || 'Sign-in failed')
      }
    },
    onError: (err: unknown) => {
      if (err instanceof ApiClientError && err.isNetworkError) setError('Cannot reach the server. Check the connection and try again.')
      else if (err instanceof ApiClientError && err.code === 'invalid_credentials') setError('Wrong username or password.')
      else setError(err instanceof Error ? err.message : 'Sign-in failed')
    },
  })

  return (
    <div className="grid min-h-screen lg:grid-cols-[3fr_2fr]">
      <section className="hidden flex-col justify-between bg-rail p-10 text-rail-foreground lg:flex">
        <div className="flex items-center gap-3 text-lg font-extrabold tracking-tight">
          <BrandMark /> Elevon POS
        </div>
        <div className="max-w-md space-y-6">
          <h1 className="text-[2.75rem] font-extrabold leading-[1.05] tracking-tight">
            The till for an LPG counter.
          </h1>
          <ul className="space-y-3 text-rail-muted">
            {FEATURES.map((f) => (
              <li key={f.text} className="flex items-start gap-3">
                <f.icon className="mt-0.5 h-5 w-5 shrink-0 text-primary" />
                <span>{f.text}</span>
              </li>
            ))}
          </ul>
        </div>
        <p className="text-xs font-semibold text-rail-muted">Elevon Systems</p>
      </section>

      <section className="flex items-center justify-center bg-background p-6">
        <form
          className="w-full max-w-sm space-y-5 rounded-lg border border-border bg-card p-6"
          onSubmit={(e) => {
            e.preventDefault()
            setError('')
            login.mutate(form)
          }}
        >
          {/* On a phone the navy panel is not rendered, so the mark comes
              here instead — otherwise sign-in is a blank white page. */}
          <div className="flex items-center gap-3 text-lg font-extrabold tracking-tight lg:hidden">
            <BrandMark /> Elevon POS
          </div>
          <div className="space-y-1">
            <h2 className="text-2xl font-bold tracking-tight">Sign in</h2>
            <p className="text-sm text-muted-foreground">Use your staff username or email.</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="username">Username or email</Label>
            <div className="relative">
              <UserIcon className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input id="username" className="pl-9" autoComplete="username" autoFocus value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })} />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">Password</Label>
            <div className="relative">
              <Lock className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input id="password" type={showPassword ? 'text' : 'password'} className="pl-9 pr-10" autoComplete="current-password"
                value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
              <button type="button" tabIndex={-1} aria-label={showPassword ? 'Hide password' : 'Show password'}
                onClick={() => setShowPassword((s) => !s)}
                className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:text-foreground">
                {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </div>
          <div className="text-right">
            <Link to="/forgot-password" className="text-sm font-semibold text-muted-foreground underline underline-offset-2 hover:text-foreground">
              Forgot password?
            </Link>
          </div>
          {error && (
            <div className="rounded-md border border-destructive/30 bg-destructive-soft px-3 py-2 text-sm font-semibold text-destructive-ink">
              {error}
            </div>
          )}
          <Button type="submit" size="lg" className="w-full" disabled={login.isPending || !form.username || !form.password}>
            {login.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Sign in'}
          </Button>
        </form>
      </section>
    </div>
  )
}
