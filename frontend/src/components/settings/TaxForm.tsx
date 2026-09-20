import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field } from '@/components/settings/Field'
import { useSaveSettings } from '@/components/settings/useSettings'
import { taxFromSettings, taxSchema, taxToPatch, type TaxFormValues } from '@/lib/settings'
import type { AppSettings } from '@/types'

const RATE_FIELDS: { name: keyof TaxFormValues; label: string; hint?: string }[] = [
  { name: 'tax_rate_cash', label: 'Cash sales tax %' },
  { name: 'tax_rate_card', label: 'Card sales tax %' },
  { name: 'tax_rate_online', label: 'Online sales tax %' },
  { name: 'tax_rate_credit', label: 'Credit sales tax %', hint: 'Leave blank to use the cash rate' },
  { name: 'further_tax_rate', label: 'Further tax %', hint: 'Applied only to unregistered buyers when FBR DI is on. Leave at 0 unless the tax advisor says otherwise.' },
]

export function TaxForm({ settings }: { settings: AppSettings }) {
  const save = useSaveSettings('Tax rates')
  const form = useForm<TaxFormValues>({ resolver: zodResolver(taxSchema), defaultValues: taxFromSettings(settings) })
  useEffect(() => form.reset(taxFromSettings(settings)), [settings, form])
  const { errors, isDirty } = form.formState

  return (
    <Card>
      <CardHeader>
        <CardTitle>Tax</CardTitle>
        <CardDescription>
          Rates are applied per line at invoice time and snapshotted on the invoice. Enter the figures from your tax advisor; they ship as 0.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4 md:grid-cols-2" onSubmit={form.handleSubmit((v) => save.mutate(taxToPatch(v)))}>
          {RATE_FIELDS.map((f) => (
            <Field key={f.name} label={f.label} htmlFor={f.name} hint={f.hint} error={errors[f.name]?.message}>
              <Input id={f.name} inputMode="decimal" placeholder="0" {...form.register(f.name)} />
            </Field>
          ))}
          <Field
            label="Default HS code"
            htmlFor="default_hs_code"
            error={errors.default_hs_code?.message}
            hint="LPG is 2711.1910 under the Pakistan Customs Tariff. Confirm with the advisor before enabling FBR DI."
          >
            <Input id="default_hs_code" placeholder="2711.1910" {...form.register('default_hs_code')} />
          </Field>
          <div className="flex items-end justify-end md:col-span-2">
            <Button type="submit" disabled={!isDirty || save.isPending}>
              {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save tax settings'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
