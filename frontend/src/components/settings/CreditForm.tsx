import { useEffect } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useSaveSettings } from '@/components/settings/useSettings'
import { creditSchema, type CreditFormValues } from '@/lib/settings'
import type { AppSettings } from '@/types'

export function CreditForm({ settings }: { settings: AppSettings }) {
  const save = useSaveSettings('Credit settings')
  const values = (s: AppSettings): CreditFormValues => ({ credit_limit_enforced: s.credit_limit_enforced })
  const form = useForm<CreditFormValues>({ resolver: zodResolver(creditSchema), defaultValues: values(settings) })
  useEffect(() => form.reset(values(settings)), [settings, form])
  const { isDirty } = form.formState

  return (
    <Card>
      <CardHeader>
        <CardTitle>Credit</CardTitle>
        <CardDescription>Credit limits are set per customer. Enforcing them blocks a credit sale that would exceed the limit unless an admin overrides with their PIN.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="flex items-center justify-between gap-4" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
          <div className="flex items-center gap-3">
            <Controller
              control={form.control}
              name="credit_limit_enforced"
              render={({ field }) => <Switch id="credit_limit_enforced" checked={field.value} onCheckedChange={field.onChange} />}
            />
            <Label htmlFor="credit_limit_enforced">Enforce customer credit limits at the till</Label>
          </div>
          <Button type="submit" disabled={!isDirty || save.isPending}>
            {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save'}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}
