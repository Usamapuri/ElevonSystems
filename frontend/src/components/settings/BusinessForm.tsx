import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Field } from '@/components/settings/Field'
import { useSaveSettings } from '@/components/settings/useSettings'
import { businessFromSettings, businessSchema, type BusinessFormValues } from '@/lib/settings'
import type { AppSettings } from '@/types'

export function BusinessForm({ settings }: { settings: AppSettings }) {
  const save = useSaveSettings('Business details')
  const form = useForm<BusinessFormValues>({ resolver: zodResolver(businessSchema), defaultValues: businessFromSettings(settings) })
  useEffect(() => form.reset(businessFromSettings(settings)), [settings, form])
  const { errors, isDirty } = form.formState

  return (
    <Card>
      <CardHeader>
        <CardTitle>Business</CardTitle>
        <CardDescription>Printed on every invoice and sent to FBR once Digital Invoicing is enabled.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4 md:grid-cols-2" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
          <div className="md:col-span-2">
            <Field label="Business name" htmlFor="business_name" error={errors.business_name?.message}>
              <Input id="business_name" {...form.register('business_name')} />
            </Field>
          </div>
          <div className="md:col-span-2">
            <Field label="Address" htmlFor="business_address" error={errors.business_address?.message}>
              <Textarea id="business_address" rows={2} {...form.register('business_address')} />
            </Field>
          </div>
          <Field label="Phone" htmlFor="business_phone" error={errors.business_phone?.message}>
            <Input id="business_phone" {...form.register('business_phone')} />
          </Field>
          <Field label="Province" htmlFor="business_province" error={errors.business_province?.message} hint="As FBR names it, e.g. Punjab">
            <Input id="business_province" {...form.register('business_province')} />
          </Field>
          <Field label="NTN" htmlFor="business_ntn" error={errors.business_ntn?.message}>
            <Input id="business_ntn" {...form.register('business_ntn')} />
          </Field>
          <Field label="STRN" htmlFor="business_strn" error={errors.business_strn?.message}>
            <Input id="business_strn" {...form.register('business_strn')} />
          </Field>
          <Field
            label="Day boundary hour"
            htmlFor="day_boundary_hour"
            error={errors.day_boundary_hour?.message}
            hint="Sales before this hour count for the previous business day. 0 = midnight."
          >
            <Input id="day_boundary_hour" type="number" min={0} max={12} {...form.register('day_boundary_hour', { valueAsNumber: true })} />
          </Field>
          <div className="flex items-end justify-end md:col-span-2">
            <Button type="submit" disabled={!isDirty || save.isPending}>
              {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save business details'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
