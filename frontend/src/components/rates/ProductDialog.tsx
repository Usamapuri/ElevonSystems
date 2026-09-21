import { useEffect } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { z } from 'zod'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Field } from '@/components/settings/Field'
import { toast } from '@/hooks/use-toast'
import type { Product } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Present when editing; omitted for "Add product". */
  product?: Product
}

const HS_CODE_RE = /^\d{4}\.\d{4}$/
const RATE_RE = /^\d+(\.\d{1,2})?$/

const schema = z
  .object({
    mode: z.enum(['create', 'edit']),
    name: z.string().trim().min(1, 'Name is required').max(120, 'At most 120 characters'),
    sku: z.string().trim().max(40, 'At most 40 characters'),
    rate: z.string().trim(),
    hs_code: z.string().trim(),
    sort_order: z.string().trim(),
    is_active: z.boolean(),
  })
  .superRefine((v, ctx) => {
    if (v.hs_code !== '' && !HS_CODE_RE.test(v.hs_code)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['hs_code'], message: 'Use the form 0000.0000' })
    }
    if (v.mode === 'create') {
      if (v.sort_order !== '' && !/^-?\d+$/.test(v.sort_order)) {
        ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['sort_order'], message: 'Whole number' })
      }
      if (v.rate === '' || !RATE_RE.test(v.rate) || Number(v.rate) < 0) {
        ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['rate'], message: 'Zero or more, at most 2 decimal places' })
      }
    }
  })
type Values = z.infer<typeof schema>

const EMPTY: Values = { mode: 'create', name: '', sku: '', rate: '', hs_code: '', sort_order: '0', is_active: true }

function fromProduct(p: Product): Values {
  return { mode: 'edit', name: p.name, sku: p.sku ?? '', rate: '', hs_code: p.hs_code ?? '', sort_order: String(p.sort_order), is_active: p.is_active }
}

function apiMessage(err: unknown): { field?: 'name' | 'sku' | 'rate' | 'hs_code' | 'root'; message: string } {
  if (err instanceof ApiClientError) {
    switch (err.code) {
      case 'product_name_taken':
        return { field: 'name', message: 'A product with that name already exists' }
      case 'sku_taken':
        return { field: 'sku', message: 'That SKU is already used by another product' }
      case 'invalid_hs_code':
        return { field: 'hs_code', message: 'HS code must be blank or in the form 0000.0000' }
      case 'invalid_rate':
        return { field: 'rate', message: 'Rate must be zero or more with at most 2 decimal places' }
      case 'no_changes':
        return { field: 'root', message: 'Nothing to update' }
    }
    return { field: 'root', message: err.message }
  }
  return { field: 'root', message: 'Something went wrong' }
}

export function ProductDialog({ open, onOpenChange, product }: Props) {
  const qc = useQueryClient()
  const editing = !!product
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: product ? fromProduct(product) : EMPTY })
  useEffect(() => {
    if (open) form.reset(product ? fromProduct(product) : EMPTY)
  }, [open, product, form])

  const mutation = useMutation({
    mutationFn: async (v: Values) => {
      if (product) {
        return apiClient.updateProduct(product.id, {
          name: v.name,
          sku: v.sku,
          hs_code: v.hs_code,
          is_active: v.is_active,
        })
      }
      return apiClient.createProduct({
        name: v.name,
        sku: v.sku,
        rate: Number(v.rate),
        hs_code: v.hs_code,
        sort_order: v.sort_order === '' ? undefined : Number(v.sort_order),
      })
    },
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['products'] })
      qc.invalidateQueries({ queryKey: ['rate-history'] })
      toast({ title: product ? 'Product updated' : 'Product created', description: res.data?.name, variant: 'success' })
      onOpenChange(false)
    },
    onError: (err: unknown) => {
      const m = apiMessage(err)
      form.setError(m.field ?? 'root', { message: m.message })
    },
  })
  const { errors } = form.formState

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{editing ? `Edit ${product.name}` : 'Add product'}</DialogTitle>
            <DialogDescription>
              {editing ? 'Rate changes happen from the rates table below, so every change is logged with history.' : 'Every product is sold by weight, in kg.'}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name" htmlFor="name" error={errors.name?.message}>
              <Input id="name" autoFocus {...form.register('name')} />
            </Field>
            <Field label="SKU (optional)" htmlFor="sku" error={errors.sku?.message}>
              <Input id="sku" autoComplete="off" {...form.register('sku')} />
            </Field>
            {!editing && (
              <Field label="Rate (Rs / kg)" htmlFor="rate" error={errors.rate?.message}>
                <Input id="rate" type="number" step="0.01" min="0" inputMode="decimal" {...form.register('rate')} />
              </Field>
            )}
            <Field label="HS code (optional)" htmlFor="hs_code" error={errors.hs_code?.message} hint="Format 0000.0000, e.g. 2711.1910">
              <Input id="hs_code" autoComplete="off" placeholder="2711.1910" {...form.register('hs_code')} />
            </Field>
            {!editing && (
              <Field label="Sort order" htmlFor="sort_order" error={errors.sort_order?.message} hint="Lower numbers show first on the till">
                <Input id="sort_order" type="number" step="1" {...form.register('sort_order')} />
              </Field>
            )}
            {editing && (
              <div className="flex items-center gap-3 sm:col-span-2">
                <Controller
                  control={form.control}
                  name="is_active"
                  render={({ field }) => <Switch id="is_active" checked={field.value} onCheckedChange={field.onChange} />}
                />
                <Label htmlFor="is_active">Active (shown on the till and the rates table)</Label>
              </div>
            )}
          </div>
          {errors.root && <p className="text-sm text-destructive">{errors.root.message}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : editing ? 'Save changes' : 'Add product'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
