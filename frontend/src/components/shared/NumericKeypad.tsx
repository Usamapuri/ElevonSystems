/**
 * Touch keypad for the till. Ported from the retail counter's keypad and
 * widened for a weight pad: an LPG sale is "12.500 kg", not "3 burgers", so
 * this version carries a decimal key and a decimal-place budget.
 *
 * It is a controlled component over a raw string, not a number: "12." is a
 * real state of a half-typed weight that Number() would silently flatten.
 * Callers parse with lib/weight.ts when they need a value.
 */
import { Delete } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface Props {
  value: string
  onChange: (next: string) => void
  /** Decimal places allowed after the point. 0 hides the decimal key. */
  maxDecimals?: number
  /** Total characters, decimal point included. */
  maxLength?: number
  /** Rendered on the bottom-left key; omitted, that key is a spacer. */
  onEnter?: () => void
  enterLabel?: string
  className?: string
}

const DIGITS = ['1', '2', '3', '4', '5', '6', '7', '8', '9'] as const

export function NumericKeypad({
  value,
  onChange,
  maxDecimals = 3,
  maxLength = 12,
  onEnter,
  enterLabel = 'Enter',
  className,
}: Props) {
  const decimals = () => {
    const i = value.indexOf('.')
    return i === -1 ? 0 : value.length - i - 1
  }

  const appendDigit = (d: string) => {
    if (value.length >= maxLength) return
    if (value.includes('.') && decimals() >= maxDecimals) return
    // A lone leading zero is a placeholder, not a digit: 0 then 5 is 5, but
    // 0 then . then 5 is 0.5, which the decimal branch handles.
    if (value === '0') {
      onChange(d)
      return
    }
    onChange(value + d)
  }

  const appendDot = () => {
    if (maxDecimals <= 0 || value.includes('.')) return
    if (value.length + 1 > maxLength) return
    onChange(value === '' ? '0.' : `${value}.`)
  }

  const backspace = () => onChange(value.slice(0, -1))
  const clear = () => onChange('')

  return (
    <div className={cn('select-none', className)}>
      <div className="grid grid-cols-3 gap-2">
        {DIGITS.map((d) => (
          <Button
            key={d}
            type="button"
            variant="secondary"
            className="h-14 min-h-[48px] text-xl font-semibold"
            onClick={() => appendDigit(d)}
          >
            {d}
          </Button>
        ))}
        <Button
          type="button"
          variant="outline"
          className="h-14 min-h-[48px] text-xl font-semibold"
          disabled={maxDecimals <= 0}
          onClick={appendDot}
          aria-label="Decimal point"
        >
          .
        </Button>
        <Button
          type="button"
          variant="secondary"
          className="h-14 min-h-[48px] text-xl font-semibold"
          onClick={() => appendDigit('0')}
        >
          0
        </Button>
        <Button
          type="button"
          variant="outline"
          className="h-14 min-h-[48px]"
          onClick={backspace}
          aria-label="Backspace"
        >
          <Delete className="mx-auto h-6 w-6" />
        </Button>
      </div>
      <div className="mt-2 flex gap-2">
        <Button type="button" variant="ghost" className="h-11 flex-1" onClick={clear}>
          Clear
        </Button>
        {onEnter && (
          <Button type="button" className="h-11 flex-1" onClick={onEnter}>
            {enterLabel}
          </Button>
        )}
      </div>
    </div>
  )
}
