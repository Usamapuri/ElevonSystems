/**
 * One FBR environment's card on Settings → FBR: its token slot and a Run
 * test button (spec 2026-10-12 §2.7). The sandbox test files a test
 * invoice; the production test only validates, and says so.
 */
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from '@/hooks/use-toast'
import type { FiscalEnvironment, FiscalTestResult, FiscalTokenState } from '@/types'

export const FISCAL_STATUS_KEY = ['fiscal', 'status'] as const

interface Props {
  environment: FiscalEnvironment
  token: FiscalTokenState
  /** This environment is the one live filing uses right now. */
  active: boolean
  onResult: (result: FiscalTestResult) => void
}

const TITLES: Record<FiscalEnvironment, { title: string; description: string }> = {
  sandbox: { title: 'Sandbox', description: 'The PRAL test gateway. A test here files a test invoice and returns its number.' },
  production: { title: 'Production', description: 'The live FBR gateway against the store’s NTN. A test here validates only — nothing is filed.' },
}

export function FiscalEnvCard({ environment, token, active, onResult }: Props) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState('')
  const [confirmRemove, setConfirmRemove] = useState(false)
  const errorMessage = (err: unknown, fallback: string) => (err instanceof Error ? err.message : fallback)

  const save = useMutation({
    mutationFn: () => apiClient.saveFiscalToken(environment, draft.trim()),
    onSuccess: () => {
      setDraft('')
      qc.invalidateQueries({ queryKey: FISCAL_STATUS_KEY })
      toast({ title: `${TITLES[environment].title} token saved`, variant: 'success' })
    },
    onError: (err: unknown) => toast({ title: 'Could not save the token', description: errorMessage(err, 'Unknown error'), variant: 'destructive' }),
  })
  const remove = useMutation({
    mutationFn: () => apiClient.deleteFiscalToken(environment),
    onSuccess: () => {
      setConfirmRemove(false)
      qc.invalidateQueries({ queryKey: FISCAL_STATUS_KEY })
      toast({ title: `${TITLES[environment].title} token removed`, variant: 'success' })
    },
    onError: (err: unknown) => {
      setConfirmRemove(false)
      const code = err instanceof ApiClientError ? err.code : undefined
      toast({
        title: code === 'fiscal_enabled' ? 'Switch filing off first' : 'Could not remove the token',
        description: errorMessage(err, 'Unknown error'),
        variant: 'destructive',
      })
    },
  })
  const test = useMutation({
    mutationFn: () => apiClient.runFiscalTest(environment),
    onSuccess: (res) => {
      if (res.data) onResult(res.data)
    },
    onError: (err: unknown) => toast({ title: 'The test could not run', description: errorMessage(err, 'Unknown error'), variant: 'destructive' }),
  })

  const t = TITLES[environment]
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {t.title}
          {active && <Badge>In use</Badge>}
        </CardTitle>
        <CardDescription>{t.description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm">
          Token: {token.set ? <span className="tabular font-semibold">{token.masked || '(saved)'}</span> : <span className="text-muted-foreground">No token saved</span>}
        </p>
        <form
          className="flex flex-col gap-2 sm:flex-row sm:items-end"
          onSubmit={(e) => {
            e.preventDefault()
            if (draft.trim().length >= 8) save.mutate()
          }}
        >
          <div className="flex-1 space-y-1.5">
            <Label htmlFor={`token-${environment}`}>{token.set ? 'Replace token' : 'Token'}</Label>
            <Input
              id={`token-${environment}`}
              type="password"
              autoComplete="off"
              placeholder="Paste the IRIS token"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
          </div>
          <Button type="submit" disabled={draft.trim().length < 8 || save.isPending}>
            {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save token'}
          </Button>
        </form>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="button" variant="secondary" disabled={!token.set || test.isPending} onClick={() => test.mutate()}>
            {test.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Run test'}
          </Button>
          {environment === 'production' && <span className="text-xs text-muted-foreground">Validates only — nothing is filed.</span>}
          {token.set && !confirmRemove && (
            <Button type="button" variant="ghost" className="ml-auto text-destructive" onClick={() => setConfirmRemove(true)}>
              Remove
            </Button>
          )}
          {confirmRemove && (
            <span className="ml-auto flex items-center gap-2 text-sm">
              Remove the {t.title.toLowerCase()} token?
              <Button type="button" size="sm" variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate()}>
                Remove
              </Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setConfirmRemove(false)}>
                Keep
              </Button>
            </span>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
