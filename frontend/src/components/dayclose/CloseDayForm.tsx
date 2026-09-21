/**
 * Counting the day shut (spec §6.7): cash, card and online, all three, then
 * seal.
 *
 * Ported from the retail counter's CloseDayWizard and collapsed from a
 * five-step wizard into one form. Staging is gone with it — there is no
 * "cash counted, waiting for a manager to lock it" state here, because the
 * person counting is the person closing.
 *
 * What survived the port, because the retail POS found out why it mattered:
 *
 *  - Every tender is countable on a touchscreen. The keypad drives whichever
 *    box is selected, so card and online are as enterable as cash; a till
 *    with no keyboard used to have no way to type them.
 *  - No box carries `placeholder="0"`. A grey zero reads as a counted zero.
 *  - There is no Skip. A NULL counted column can only ever mean
 *    "force-closed, nobody counted", never "counted, and it was empty".
 *
 * The note gate mirrors `dayops.Close` rather than guessing: the arithmetic
 * lives in dayClose.ts and is tested against the server's own comparison, so
 * this form never demands a note the server would have waved through, or
 * lets one through that it would refuse with `variance_note_required`.
 */
import { useState } from 'react'
import { AlertTriangle, Loader2, Lock } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { NumericKeypad } from '@/components/shared/NumericKeypad'
import { formatMoney } from '@/lib/money'
import { cn } from '@/lib/utils'
import { emptyCounts, evaluateClose, tenderLabel, type CountedInputs, type TenderKey } from '@/components/dayclose/dayClose'
import type { CloseDayRequest, DayExpected } from '@/types'

interface Props {
  expected: DayExpected | null
  /** settings.day_close_variance_threshold; the server enforces it too. */
  threshold: number
  pending: boolean
  error: string | null
  onSubmit: (payload: CloseDayRequest) => void
}

/** A signed rupee figure: `+150`, `-50`, `0`. */
function signedMoney(n: number): string {
  if (n === 0) return formatMoney(0)
  return (n > 0 ? '+' : '-') + formatMoney(Math.abs(n))
}

export function CloseDayForm({ expected, threshold, pending, error, onSubmit }: Props) {
  const [counted, setCounted] = useState<CountedInputs>(emptyCounts)
  const [active, setActive] = useState<TenderKey>('cash')
  const [notes, setNotes] = useState('')

  const evaluation = evaluateClose({ expected, counted, threshold, notes })
  const noteMissing = evaluation.noteRequired && notes.trim() === ''

  const setTender = (tender: TenderKey, raw: string) =>
    setCounted((prev) => ({ ...prev, [tender]: raw.replace(/[^0-9.]/g, '') }))

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Lock className="h-5 w-5" /> Close the day
        </CardTitle>
        <CardDescription>
          Count all three tenders — enter 0 for one that took nothing. Credit sales are not counted here: no money
          arrived in the till for them.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-4 lg:grid-cols-[1fr_260px]">
          <div className="space-y-3">
            {evaluation.tenders.map((t) => (
              <div
                key={t.tender}
                className={cn(
                  'rounded-lg border p-3',
                  active === t.tender ? 'border-ring ring-1 ring-ring/40' : 'border-border',
                  t.overThreshold && 'border-warning/60',
                )}
              >
                <div className="flex items-center justify-between gap-3">
                  <Label htmlFor={`counted-${t.tender}`} className="text-base">
                    {tenderLabel(t.tender)}
                  </Label>
                  <span className="text-sm text-muted-foreground">
                    Expected <span className="tabular">{formatMoney(t.expected)}</span>
                  </span>
                </div>
                <Input
                  id={`counted-${t.tender}`}
                  inputMode="decimal"
                  autoComplete="off"
                  value={counted[t.tender]}
                  onFocus={() => setActive(t.tender)}
                  onChange={(e) => setTender(t.tender, e.target.value)}
                  className="mt-2 h-14 text-center text-2xl font-semibold tabular"
                />
                <p
                  className={cn(
                    'mt-1 h-5 text-sm tabular',
                    t.variance === null
                      ? 'text-muted-foreground'
                      : t.overThreshold
                        ? 'font-medium text-warning-ink'
                        : t.variance === 0
                          ? 'text-success-ink'
                          : 'text-muted-foreground',
                  )}
                >
                  {t.variance === null
                    ? 'Not counted yet'
                    : t.variance === 0
                      ? 'Matches exactly'
                      : `${signedMoney(t.variance)} against expected`}
                </p>
              </div>
            ))}
          </div>

          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">
              Keypad enters <span className="font-medium text-foreground">{tenderLabel(active)}</span>
            </p>
            <NumericKeypad
              value={counted[active]}
              onChange={(next) => setTender(active, next)}
              maxDecimals={2}
              maxLength={11}
            />
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="closing-notes">
            Closing notes{evaluation.noteRequired ? '' : ' (optional)'}
          </Label>
          <Textarea
            id="closing-notes"
            rows={3}
            maxLength={2000}
            value={notes}
            placeholder={
              evaluation.noteRequired
                ? 'What explains the difference?'
                : 'Anything worth reading back later'
            }
            onChange={(e) => setNotes(e.target.value)}
            className={cn(noteMissing && 'border-warning focus-visible:ring-warning')}
          />
          {evaluation.noteRequired && (
            <p className="flex items-start gap-2 text-sm text-warning-ink">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>
                A tender is off by more than {formatMoney(threshold)}. Write what explains it — the close is refused
                without a note.
              </span>
            </p>
          )}
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <div className="flex flex-wrap items-center justify-end gap-3">
          {!evaluation.complete && (
            <p className="mr-auto text-sm text-muted-foreground">Count all three tenders to close.</p>
          )}
          <Button
            size="lg"
            disabled={!evaluation.canSubmit || pending}
            onClick={() => {
              if (!evaluation.payload || pending) return
              onSubmit(evaluation.payload)
            }}
          >
            {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Close the day
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
