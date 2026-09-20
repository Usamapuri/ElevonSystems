import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field } from '@/components/settings/Field'
import { useSaveSettings } from '@/components/settings/useSettings'
import { dayCloseSchema, type DayCloseFormValues } from '@/lib/settings'
import type { AppSettings } from '@/types'

export function DayCloseForm({ settings }: { settings: AppSettings }) {
  const save = useSaveSettings('Day close settings')
  const values = (s: AppSettings): DayCloseFormValues => ({ day_close_variance_threshold: s.day_close_variance_threshold })
  const form = useForm<DayCloseFormValues>({ resolver: zodResolver(dayCloseSchema), defaultValues: values(settings) })
  useEffect(() => form.reset(values(settings)), [settings, form])
  const { errors, isDirty } = form.formState

  return (
    <Card>
      <CardHeader>
        <CardTitle>Day close</CardTitle>
        <CardDescription>When counted cash, card or online differs from expected by more than this, the person closing must write a note.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4 md:grid-cols-2" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
          <Field label="Variance threshold (Rs)" htmlFor="day_close_variance_threshold" error={errors.day_close_variance_threshold?.message}>
            <Input id="day_close_variance_threshold" type="number" min={0} step="1" {...form.register('day_close_variance_threshold', { valueAsNumber: true })} />
          </Field>
          <div className="flex items-end justify-end">
            <Button type="submit" disabled={!isDirty || save.isPending}>
              {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
