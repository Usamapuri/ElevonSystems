import { useEffect } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Field } from '@/components/settings/Field'
import { toast } from '@/hooks/use-toast'
import { BUYER_REGISTRATION_TYPES, customerDialogSchema, type CustomerDialogValues } from '@/lib/customerSchemas'
import type { Customer } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Present when editing; omitted for "New customer". */
  customer?: Customer
}

const EMPTY: CustomerDialogValues = {
  name: '',
  phone: '',
  ntn: '',
  cnic: '',
  buyer_registration_type: 'Unregistered',
  address: '',
  province: '',
  credit_allowed: false,
  credit_limit: '',
  notes: '',
  is_active: true,
}

function fromCustomer(c: Customer): CustomerDialogValues {
  return {
    name: c.name,
    phone: c.phone ?? '',
    ntn: c.ntn ?? '',
    cnic: c.cnic ?? '',
    buyer_registration_type: c.buyer_registration_type,
    address: c.address ?? '',
    province: c.province ?? '',
    credit_allowed: c.credit_allowed,
    credit_limit: c.credit_limit === null ? '' : String(c.credit_limit),
    notes: c.notes ?? '',
    is_active: c.is_active,
  }
}

type FieldName = 'name' | 'phone' | 'ntn' | 'cnic' | 'buyer_registration_type' | 'province' | 'credit_limit' | 'root'

function apiMessage(err: unknown): { field?: FieldName; message: string } {
  if (err instanceof ApiClientError) {
    switch (err.code) {
      case 'invalid_name':
        return { field: 'name', message: 'Name is required and at most 120 characters' }
      case 'invalid_phone':
        return { field: 'phone', message: 'Phone must be digits, + or spaces, at most 30 characters' }
      case 'phone_taken':
        return { field: 'phone', message: 'A customer with that phone number already exists' }
      case 'invalid_ntn':
        return { field: 'ntn', message: 'NTN must be digits or dashes, at most 20 characters' }
      case 'invalid_cnic':
        return { field: 'cnic', message: 'CNIC must be digits only, at most 15 characters' }
      case 'invalid_buyer_registration_type':
        return { field: 'buyer_registration_type', message: 'Choose Registered or Unregistered' }
      case 'invalid_province':
        return { field: 'province', message: 'Province is at most 40 characters' }
      case 'invalid_credit_limit':
        return { field: 'credit_limit', message: 'Credit limit must be zero or more with at most 2 decimal places' }
      case 'no_changes':
        return { field: 'root', message: 'Nothing to update' }
      case 'customer_not_found':
        return { field: 'root', message: 'This customer no longer exists' }
    }
    return { field: 'root', message: err.message }
  }
  return { field: 'root', message: 'Something went wrong' }
}

export function CustomerDialog({ open, onOpenChange, customer }: Props) {
  const qc = useQueryClient()
  const editing = !!customer
  const form = useForm<CustomerDialogValues>({ resolver: zodResolver(customerDialogSchema), defaultValues: customer ? fromCustomer(customer) : EMPTY })
  useEffect(() => {
    if (open) form.reset(customer ? fromCustomer(customer) : EMPTY)
  }, [open, customer, form])

  const mutation = useMutation({
    mutationFn: async (v: CustomerDialogValues) => {
      const payload = {
        name: v.name,
        phone: v.phone,
        ntn: v.ntn,
        cnic: v.cnic,
        buyer_registration_type: v.buyer_registration_type,
        address: v.address,
        province: v.province,
        credit_allowed: v.credit_allowed,
        credit_limit: v.credit_limit === '' ? undefined : Number(v.credit_limit),
        notes: v.notes,
      }
      if (customer) {
        return apiClient.updateCustomer(customer.id, { ...payload, is_active: v.is_active })
      }
      return apiClient.createCustomer(payload)
    },
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['customers'] })
      if (customer) qc.invalidateQueries({ queryKey: ['customer', customer.id] })
      toast({ title: customer ? 'Customer updated' : 'Customer created', description: res.data?.name, variant: 'success' })
      onOpenChange(false)
    },
    onError: (err: unknown) => {
      const m = apiMessage(err)
      form.setError(m.field ?? 'root', { message: m.message })
    },
  })
  const { errors } = form.formState
  const creditAllowed = form.watch('credit_allowed')

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{editing ? `Edit ${customer.name}` : 'New customer'}</DialogTitle>
            <DialogDescription>
              NTN, CNIC, buyer type and province feed FBR Digital Invoicing once it is configured.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name" htmlFor="name" error={errors.name?.message}>
              <Input id="name" autoFocus {...form.register('name')} />
            </Field>
            <Field label="Phone (optional)" htmlFor="phone" error={errors.phone?.message}>
              <Input id="phone" autoComplete="off" placeholder="0300 1234567" {...form.register('phone')} />
            </Field>
            <Field label="NTN (optional)" htmlFor="ntn" error={errors.ntn?.message}>
              <Input id="ntn" autoComplete="off" {...form.register('ntn')} />
            </Field>
            <Field label="CNIC (optional)" htmlFor="cnic" error={errors.cnic?.message}>
              <Input id="cnic" autoComplete="off" inputMode="numeric" {...form.register('cnic')} />
            </Field>
            <Field label="Buyer registration type" htmlFor="buyer_registration_type" error={errors.buyer_registration_type?.message}>
              <Controller
                control={form.control}
                name="buyer_registration_type"
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="buyer_registration_type">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {BUYER_REGISTRATION_TYPES.map((t) => (
                        <SelectItem key={t} value={t}>
                          {t}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
            </Field>
            <Field label="Province (optional)" htmlFor="province" error={errors.province?.message}>
              <Input id="province" autoComplete="off" {...form.register('province')} />
            </Field>
            <Field label="Address (optional)" htmlFor="address" error={errors.address?.message}>
              <Textarea id="address" rows={2} {...form.register('address')} />
            </Field>
            <Field label="Notes (optional)" htmlFor="notes" error={errors.notes?.message}>
              <Textarea id="notes" rows={2} {...form.register('notes')} />
            </Field>
            <div className="flex items-center gap-3">
              <Controller
                control={form.control}
                name="credit_allowed"
                render={({ field }) => <Switch id="credit_allowed" checked={field.value} onCheckedChange={field.onChange} />}
              />
              <Label htmlFor="credit_allowed">Credit allowed</Label>
            </div>
            <Field
              label="Credit limit (optional)"
              htmlFor="credit_limit"
              error={errors.credit_limit?.message}
              hint={creditAllowed ? 'Blank means no limit' : 'Only used while credit allowed is on'}
            >
              <Input id="credit_limit" type="number" step="0.01" min="0" inputMode="decimal" {...form.register('credit_limit')} />
            </Field>
            {editing && (
              <div className="flex items-center gap-3 sm:col-span-2">
                <Controller
                  control={form.control}
                  name="is_active"
                  render={({ field }) => <Switch id="is_active" checked={field.value} onCheckedChange={field.onChange} />}
                />
                <Label htmlFor="is_active">Active</Label>
              </div>
            )}
          </div>
          {errors.root && <p className="text-sm text-destructive">{errors.root.message}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : editing ? 'Save changes' : 'Create customer'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
