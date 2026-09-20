/**
 * Admin PIN gate. Ported from the retail counter's void modal and stripped
 * back to the one thing it is actually for here: collect a 4-digit PIN (and,
 * when the caller asks, a written reason) and hand them to the caller, which
 * owns the request.
 *
 * Two places in the till need it, and they need different requests — the
 * credit-limit override retries POST /invoices with the PIN in the body, the
 * void posts /invoices/:id/void — so this component deliberately does no
 * API work of its own. It shows the caller's error, shakes, and clears the
 * boxes so the next attempt starts clean.
 *
 * The PIN is never stored, never logged and never put in a query string.
 */
import { useEffect, useRef, useState } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

/** Matches dayops.ReasonMinLen / ReasonMaxLen — the server refuses "x". */
export const REASON_MIN = 4
export const REASON_MAX = 500

const PIN_LENGTH = 4

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  /** Show and require a written reason (voids need one). */
  withReason?: boolean
  reasonLabel?: string
  reasonPlaceholder?: string
  submitLabel?: string
  submitVariant?: 'default' | 'destructive'
  pending?: boolean
  /** Message from the caller's last attempt; drives the shake and a reset. */
  error?: string | null
  onSubmit: (pin: string, reason: string) => void
}

export function PinEntryModal({
  open,
  onOpenChange,
  title,
  description,
  withReason = false,
  reasonLabel = 'Reason',
  reasonPlaceholder = 'Why is this being done?',
  submitLabel = 'Confirm',
  submitVariant = 'default',
  pending = false,
  error = null,
  onSubmit,
}: Props) {
  const [digits, setDigits] = useState<string[]>(Array(PIN_LENGTH).fill(''))
  const [reason, setReason] = useState('')
  const [localError, setLocalError] = useState('')
  const [shake, setShake] = useState(false)
  const inputs = useRef<(HTMLInputElement | null)[]>([])

  // A fresh open is a fresh attempt: never carry a PIN across two dialogs.
  useEffect(() => {
    if (!open) return
    setDigits(Array(PIN_LENGTH).fill(''))
    setReason('')
    setLocalError('')
    const t = setTimeout(() => inputs.current[0]?.focus(), 50)
    return () => clearTimeout(t)
  }, [open])

  // The caller rejected the PIN: clear it, shake, put the cursor back.
  useEffect(() => {
    if (!error) return
    setDigits(Array(PIN_LENGTH).fill(''))
    setShake(true)
    const stop = setTimeout(() => setShake(false), 500)
    const focus = setTimeout(() => inputs.current[0]?.focus(), 80)
    return () => {
      clearTimeout(stop)
      clearTimeout(focus)
    }
  }, [error])

  const handleDigit = (index: number, raw: string) => {
    // Paste of a whole PIN: spread it across the boxes instead of losing it.
    const cleaned = raw.replace(/\D/g, '')
    if (cleaned.length > 1) {
      const next = Array(PIN_LENGTH).fill('')
      cleaned.slice(0, PIN_LENGTH).split('').forEach((d, i) => (next[i] = d))
      setDigits(next)
      setLocalError('')
      inputs.current[Math.min(cleaned.length, PIN_LENGTH - 1)]?.focus()
      return
    }
    if (!/^\d?$/.test(cleaned)) return
    const next = [...digits]
    next[index] = cleaned
    setDigits(next)
    setLocalError('')
    if (cleaned && index < PIN_LENGTH - 1) inputs.current[index + 1]?.focus()
  }

  const handleKeyDown = (index: number, e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace' && !digits[index] && index > 0) inputs.current[index - 1]?.focus()
    if (e.key === 'Enter') {
      e.preventDefault()
      submit()
    }
  }

  const trimmedReason = reason.trim()
  const reasonOk = !withReason || trimmedReason.length >= REASON_MIN
  const pin = digits.join('')
  const canSubmit = pin.length === PIN_LENGTH && reasonOk && !pending

  const submit = () => {
    if (withReason && !reasonOk) {
      setLocalError(`Write a reason of at least ${REASON_MIN} characters`)
      return
    }
    if (pin.length !== PIN_LENGTH) {
      setLocalError(`Enter the ${PIN_LENGTH}-digit PIN`)
      return
    }
    onSubmit(pin, trimmedReason)
  }

  const shown = localError || error

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>

        {withReason && (
          <div className="space-y-2">
            <Label htmlFor="pin-reason">{reasonLabel}</Label>
            <Textarea
              id="pin-reason"
              rows={2}
              maxLength={REASON_MAX}
              value={reason}
              placeholder={reasonPlaceholder}
              onChange={(e) => {
                setReason(e.target.value)
                setLocalError('')
              }}
            />
            <p className="text-xs text-muted-foreground">
              Saved to the audit log — write what you would want to read back later.
            </p>
          </div>
        )}

        <div className="space-y-2">
          <Label className="block text-center">Admin PIN</Label>
          <div className={`flex justify-center gap-3 ${shake ? 'animate-pin-shake' : ''}`}>
            {digits.map((digit, i) => (
              <input
                key={i}
                ref={(el) => {
                  inputs.current[i] = el
                }}
                type="password"
                inputMode="numeric"
                autoComplete="off"
                aria-label={`PIN digit ${i + 1}`}
                maxLength={PIN_LENGTH}
                value={digit}
                onChange={(e) => handleDigit(i, e.target.value)}
                onKeyDown={(e) => handleKeyDown(i, e)}
                className="h-14 w-12 rounded-xl border-2 border-input bg-background text-center text-2xl font-bold text-foreground focus:border-ring focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
            ))}
          </div>
          {shown && <p className="text-center text-sm text-destructive">{shown}</p>}
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button variant={submitVariant} onClick={submit} disabled={!canSubmit}>
            {pending ? 'Checking…' : submitLabel}
          </Button>
        </DialogFooter>

        <style>{`
          @keyframes pin-shake {
            0%, 100% { transform: translateX(0); }
            25% { transform: translateX(-8px); }
            75% { transform: translateX(8px); }
          }
          .animate-pin-shake { animation: pin-shake 0.3s ease-in-out 2; }
        `}</style>
      </DialogContent>
    </Dialog>
  )
}
