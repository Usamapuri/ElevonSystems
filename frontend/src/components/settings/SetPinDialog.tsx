import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import apiClient, { ApiClientError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Field } from '@/components/settings/Field'
import { toast } from '@/hooks/use-toast'
import { pinSchema, type PinValues } from '@/lib/userSchemas'
import type { User } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: User | null
}

export function SetPinDialog({ open, onOpenChange, user }: Props) {
  const qc = useQueryClient()
  const form = useForm<PinValues>({ resolver: zodResolver(pinSchema), defaultValues: { pin: '', confirm: '' } })
  useEffect(() => {
    if (open) form.reset({ pin: '', confirm: '' })
  }, [open, form])

  const set = useMutation({
    mutationFn: (v: PinValues) => apiClient.setUserPin(user!.id, v.pin),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
      toast({ title: 'PIN set', description: user ? `${user.first_name} can now authorise voids and day reopen.` : undefined, variant: 'success' })
      onOpenChange(false)
    },
    onError: (err: unknown) => {
      if (err instanceof ApiClientError && err.code === 'pin_in_use') form.setError('pin', { message: 'Another admin already uses this PIN' })
      else form.setError('root', { message: err instanceof Error ? err.message : 'Could not set the PIN' })
    },
  })
  const { errors } = form.formState

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <form onSubmit={form.handleSubmit((v) => set.mutate(v))} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{user?.has_pin ? 'Change PIN' : 'Set PIN'}{user ? ` — ${user.username}` : ''}</DialogTitle>
            <DialogDescription>Four digits. Used to approve voids, day reopen and credit-limit overrides. Must differ from every other admin's PIN.</DialogDescription>
          </DialogHeader>
          <Field label="PIN" htmlFor="pin" error={errors.pin?.message}>
            <Input id="pin" type="password" inputMode="numeric" maxLength={4} autoComplete="off" autoFocus {...form.register('pin')} />
          </Field>
          <Field label="Confirm PIN" htmlFor="confirm" error={errors.confirm?.message}>
            <Input id="confirm" type="password" inputMode="numeric" maxLength={4} autoComplete="off" {...form.register('confirm')} />
          </Field>
          {errors.root && <p className="text-sm text-red-600">{errors.root.message}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={set.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={set.isPending || !user}>
              {set.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Save PIN'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
