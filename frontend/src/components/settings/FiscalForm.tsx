/**
 * Settings → FBR (spec §3, 2026-10-12 §2.7): the seller profile and filing
 * switch saved as fiscal_config, the two token cards with their tests, and
 * the reference-list refresh. Server refusals on enabling (tax_rate_mismatch,
 * fiscal_token_missing, fiscal_reference_stale, fiscal_uom_not_allowed) are
 * shown beside the field they concern.
 */
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Field } from '@/components/settings/Field'
import { FISCAL_STATUS_KEY, FiscalEnvCard } from '@/components/settings/FiscalEnvCard'
import { FiscalTestDialog } from '@/components/settings/FiscalTestDialog'
import { SETTINGS_KEY } from '@/components/settings/useSettings'
import { toast } from '@/hooks/use-toast'
import type { FiscalConfig, FiscalStatus, FiscalTestResult } from '@/types'

type FormValues = Pick<
  FiscalConfig,
  'enabled' | 'is_sandbox' | 'seller_ntn_cnic' | 'seller_business_name' | 'seller_province' | 'seller_address' | 'rate_desc' | 'sale_type' | 'default_uom'
>

const ERROR_FIELD: Record<string, keyof FormValues | 'enabled'> = {
  tax_rate_mismatch: 'rate_desc',
  fiscal_token_missing: 'enabled',
  fiscal_reference_stale: 'enabled',
  fiscal_uom_not_allowed: 'default_uom',
}

function values(s: FiscalStatus): FormValues {
  return {
    enabled: s.enabled,
    is_sandbox: s.is_sandbox,
    seller_ntn_cnic: s.seller_ntn_cnic,
    seller_business_name: s.seller_business_name,
    seller_province: s.seller_province,
    seller_address: s.seller_address,
    rate_desc: s.rate_desc,
    sale_type: s.sale_type,
    default_uom: s.default_uom,
  }
}

export function FiscalForm() {
  const qc = useQueryClient()
  const status = useQuery({
    queryKey: FISCAL_STATUS_KEY,
    queryFn: async (): Promise<FiscalStatus> => {
      const res = await apiClient.getFiscalStatus()
      if (!res.success || !res.data) throw new Error(res.message || 'Could not load FBR settings')
      return res.data
    },
  })
  const [result, setResult] = useState<FiscalTestResult | null>(null)
  const [serverError, setServerError] = useState<{ field: string; message: string } | null>(null)
  const form = useForm<FormValues>({ defaultValues: status.data ? values(status.data) : undefined })
  useEffect(() => {
    if (status.data) form.reset(values(status.data))
  }, [status.data, form])

  const save = useMutation({
    mutationFn: (v: FormValues) => {
      const current = status.data
      if (!current) throw new Error('FBR settings have not loaded')
      const patch: FiscalConfig = {
        enabled: v.enabled,
        is_sandbox: v.is_sandbox,
        seller_ntn_cnic: v.seller_ntn_cnic.trim(),
        seller_business_name: v.seller_business_name.trim(),
        seller_province: v.seller_province,
        seller_address: v.seller_address.trim(),
        scenario_registered: current.scenario_registered,
        scenario_unregistered: current.scenario_unregistered,
        rate_desc: v.rate_desc.trim(),
        sale_type: v.sale_type.trim(),
        trans_type_id: current.trans_type_id,
        default_uom: v.default_uom.trim(),
        buyer_registration_default: current.buyer_registration_default,
        validate_url: current.validate_url,
        post_url: current.post_url,
      }
      return apiClient.updateSettings({ fiscal_config: patch })
    },
    onSuccess: (res) => {
      setServerError(null)
      if (res.data) qc.setQueryData(SETTINGS_KEY, res.data)
      qc.invalidateQueries({ queryKey: FISCAL_STATUS_KEY })
      toast({ title: 'FBR settings saved', variant: 'success' })
    },
    onError: (err: unknown) => {
      const code = err instanceof ApiClientError ? err.code : undefined
      const message = err instanceof Error ? err.message : 'Unknown error'
      if (code && ERROR_FIELD[code]) setServerError({ field: ERROR_FIELD[code], message })
      else toast({ title: 'Could not save FBR settings', description: message, variant: 'destructive' })
    },
  })
  const refresh = useMutation({
    mutationFn: () => apiClient.refreshFiscalReference(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: FISCAL_STATUS_KEY })
      toast({ title: 'FBR reference lists refreshed', variant: 'success' })
    },
    onError: (err: unknown) => toast({ title: 'Could not refresh the lists', description: err instanceof Error ? err.message : 'Unknown error', variant: 'destructive' }),
  })

  if (status.isLoading) return <p className="text-sm text-muted-foreground">Loading FBR settings…</p>
  if (status.error || !status.data) return <p className="text-sm text-destructive">{status.error instanceof Error ? status.error.message : 'Could not load FBR settings'}</p>
  const s = status.data
  const provinces = s.reference.provinces ?? []
  const uoms = s.reference.uoms ?? []
  const fieldError = (name: string) => (serverError?.field === name ? serverError.message : undefined)
  const activeEnv = form.watch('is_sandbox') ? 'sandbox' : 'production'

  return (
    <div className="space-y-4">
      {!s.secrets_key_set && (
        <p className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm">
          The server has no <code>FISCAL_SECRETS_KEY</code>. Tokens cannot be stored or read until it is set.
        </p>
      )}

      <Card>
        <CardHeader>
          <CardTitle>FBR Digital Invoicing</CardTitle>
          <CardDescription>
            Nothing is filed on its own: a sale is filed when the cashier presses Charge + FBR, or when you file a combined invoice from File with FBR.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4 md:grid-cols-2" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
            <div className="flex items-center justify-between rounded-md border border-border p-3 md:col-span-2">
              <div>
                <p className="text-sm font-semibold">Filing switched on</p>
                <p className="text-xs text-muted-foreground">Needs a token for the chosen environment and refreshed reference lists.</p>
                {fieldError('enabled') && <p className="text-sm text-destructive">{fieldError('enabled')}</p>}
              </div>
              <Switch checked={form.watch('enabled')} onCheckedChange={(v) => form.setValue('enabled', v, { shouldDirty: true })} />
            </div>
            <div className="flex items-center justify-between rounded-md border border-border p-3 md:col-span-2">
              <div>
                <p className="text-sm font-semibold">Environment: {activeEnv}</p>
                <p className="text-xs text-muted-foreground">Sandbox until the advisor signs off; production files against the store’s NTN.</p>
              </div>
              <Switch checked={!form.watch('is_sandbox')} onCheckedChange={(v) => form.setValue('is_sandbox', !v, { shouldDirty: true })} aria-label="Use production" />
            </div>
            <Field label="Seller NTN / CNIC" htmlFor="seller_ntn_cnic">
              <Input id="seller_ntn_cnic" {...form.register('seller_ntn_cnic')} />
            </Field>
            <Field label="Seller business name" htmlFor="seller_business_name">
              <Input id="seller_business_name" {...form.register('seller_business_name')} />
            </Field>
            <Field label="Seller province" htmlFor="seller_province" hint={provinces.length ? undefined : 'Refresh the reference lists to pick from FBR’s spelling.'}>
              {provinces.length ? (
                <Select value={form.watch('seller_province')} onValueChange={(v) => form.setValue('seller_province', v, { shouldDirty: true })}>
                  <SelectTrigger id="seller_province"><SelectValue placeholder="Choose" /></SelectTrigger>
                  <SelectContent>
                    {provinces.map((p) => <SelectItem key={p} value={p}>{p}</SelectItem>)}
                  </SelectContent>
                </Select>
              ) : (
                <Input id="seller_province" placeholder="PUNJAB" {...form.register('seller_province')} />
              )}
            </Field>
            <Field label="Seller address" htmlFor="seller_address">
              <Input id="seller_address" {...form.register('seller_address')} />
            </Field>
            <Field label="Rate description" htmlFor="rate_desc" error={fieldError('rate_desc')} hint="Must match the cash tax rate, e.g. 18%.">
              <Input id="rate_desc" {...form.register('rate_desc')} />
            </Field>
            <Field label="Sale type" htmlFor="sale_type">
              <Input id="sale_type" {...form.register('sale_type')} />
            </Field>
            <Field label="Default unit of measure" htmlFor="default_uom" error={fieldError('default_uom')}>
              {uoms.length ? (
                <Select value={form.watch('default_uom')} onValueChange={(v) => form.setValue('default_uom', v, { shouldDirty: true })}>
                  <SelectTrigger id="default_uom"><SelectValue placeholder="Choose" /></SelectTrigger>
                  <SelectContent>
                    {uoms.map((u) => <SelectItem key={u.id} value={u.description}>{u.description}</SelectItem>)}
                  </SelectContent>
                </Select>
              ) : (
                <Input id="default_uom" placeholder="KG" {...form.register('default_uom')} />
              )}
            </Field>
            <div className="flex items-end justify-between gap-2 md:col-span-2">
              <div className="text-xs text-muted-foreground">
                Reference lists: {s.reference.refreshed_at ? `refreshed ${new Date(s.reference.refreshed_at).toLocaleString()}` : 'never refreshed'}
                <Button type="button" variant="link" className="h-auto px-2 py-0 text-xs" disabled={refresh.isPending} onClick={() => refresh.mutate()}>
                  {refresh.isPending ? 'Refreshing…' : 'Refresh from FBR'}
                </Button>
              </div>
              <Button type="submit" disabled={!form.formState.isDirty || save.isPending}>
                {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save FBR settings'}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <div className="grid gap-4 md:grid-cols-2">
        <FiscalEnvCard environment="sandbox" token={s.tokens.sandbox} active={s.is_sandbox} onResult={setResult} />
        <FiscalEnvCard environment="production" token={s.tokens.production} active={!s.is_sandbox} onResult={setResult} />
      </div>

      <p className="text-xs text-muted-foreground">
        Queue: {s.queue.pending} waiting · {s.queue.dead} failed
        {s.queue.last_success_at ? ` · last filed ${new Date(s.queue.last_success_at).toLocaleString()}` : ''}
      </p>

      <FiscalTestDialog result={result} open={result !== null} onOpenChange={(open) => !open && setResult(null)} />
    </div>
  )
}
