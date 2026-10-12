import { useEffect } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Field } from '@/components/settings/Field'
import { useSaveSettings } from '@/components/settings/useSettings'
import { MAX_RECEIPT_LINES, receiptFromSettings, receiptSchema, receiptToPatch, type ReceiptFormValues } from '@/lib/settings'
import type { AppSettings } from '@/types'

export function ReceiptForm({ settings }: { settings: AppSettings }) {
  const save = useSaveSettings('Receipt layout')
  const form = useForm<ReceiptFormValues>({ resolver: zodResolver(receiptSchema), defaultValues: receiptFromSettings(settings) })
  useEffect(() => form.reset(receiptFromSettings(settings)), [settings, form])
  const { errors, isDirty } = form.formState

  return (
    <Card>
      <CardHeader>
        <CardTitle>Receipt</CardTitle>
        <CardDescription>Thermal paper size and the lines printed above and below every invoice. The logo is set under Business.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4 md:grid-cols-2" onSubmit={form.handleSubmit((v) => save.mutate(receiptToPatch(v)))}>
          <Field label="Paper width" htmlFor="receipt_paper_width_mm" error={errors.receipt_paper_width_mm?.message}>
            <Controller
              control={form.control}
              name="receipt_paper_width_mm"
              render={({ field }) => (
                <Select value={String(field.value)} onValueChange={(v) => field.onChange(Number(v) as 58 | 80)}>
                  <SelectTrigger id="receipt_paper_width_mm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="80">80 mm</SelectItem>
                    <SelectItem value="58">58 mm</SelectItem>
                  </SelectContent>
                </Select>
              )}
            />
          </Field>
          <Field label="Printable area (mm)" htmlFor="receipt_printable_area_mm" error={errors.receipt_printable_area_mm?.message} hint="72 for 80 mm paper, 48 for 58 mm">
            <Input id="receipt_printable_area_mm" type="number" min={40} max={80} {...form.register('receipt_printable_area_mm', { valueAsNumber: true })} />
          </Field>
          <Field label="Default document" htmlFor="receipt_default_document" error={errors.receipt_default_document?.message}>
            <Controller
              control={form.control}
              name="receipt_default_document"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="receipt_default_document">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="thermal">Thermal receipt</SelectItem>
                    <SelectItem value="a4">A4 tax invoice</SelectItem>
                  </SelectContent>
                </Select>
              )}
            />
          </Field>
          <Field label="Header lines" htmlFor="receipt_header_lines" error={errors.receipt_header_lines?.message} hint={`One per line, up to ${MAX_RECEIPT_LINES}`}>
            <Textarea id="receipt_header_lines" rows={3} {...form.register('receipt_header_lines')} />
          </Field>
          <Field label="Footer lines" htmlFor="receipt_footer_lines" error={errors.receipt_footer_lines?.message} hint={`One per line, up to ${MAX_RECEIPT_LINES}`}>
            <Textarea id="receipt_footer_lines" rows={3} {...form.register('receipt_footer_lines')} />
          </Field>
          <div className="flex items-end justify-end md:col-span-2">
            <Button type="submit" disabled={!isDirty || save.isPending}>
              {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save receipt layout'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
