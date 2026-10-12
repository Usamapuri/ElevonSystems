/**
 * The result of Settings → FBR → Run test (spec 2026-10-12 §2.7): the
 * exact outbound body, the FBR number and its QR on a sandbox pass, and the
 * raw gateway response for each call — everything needed to diff against
 * Postman. The token is never in any of it; it travels in a header the
 * result does not include.
 */
import { useEffect, useState } from 'react'
import { Download } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { prettyJson, testHeadline } from '@/lib/fiscal'
import { downloadBlob } from '@/lib/download'
import { fbrQrDataUrl } from '@/lib/print/fbrQr'
import type { FiscalTestResult } from '@/types'

interface Props {
  result: FiscalTestResult | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="space-y-1.5 rounded-md border border-border p-3">
      <h3 className="text-xs font-bold uppercase tracking-wide text-muted-foreground">{title}</h3>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      {children}
    </section>
  )
}

function Pre({ text }: { text: string }) {
  return <pre className="max-h-64 overflow-auto rounded bg-secondary p-2 text-xs leading-relaxed">{text}</pre>
}

export function FiscalTestDialog({ result, open, onOpenChange }: Props) {
  const [qr, setQr] = useState<string | null>(null)

  useEffect(() => {
    let alive = true
    setQr(null)
    if (result?.invoice_number) {
      fbrQrDataUrl(result.invoice_number)
        .then((url) => alive && setQr(url))
        .catch(() => alive && setQr(null))
    }
    return () => {
      alive = false
    }
  }, [result])

  if (!result) return null
  const headline = testHeadline(result)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Test result</DialogTitle>
          <DialogDescription>
            {result.environment === 'production'
              ? 'Validate only against the live FBR gateway: nothing is filed.'
              : 'Validate, then post, against the FBR sandbox.'}
          </DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2">
          <Badge variant={headline.passed ? 'default' : 'destructive'}>{headline.title}</Badge>
          <span className="text-xs font-semibold uppercase text-muted-foreground">{result.environment}</span>
          <span className="text-sm">{headline.detail}</span>
        </div>

        {result.request_body !== null && result.request_body !== undefined && (
          <Section title="Outbound body" hint={result.steps.map((s) => `${s.phase.toUpperCase()} ${s.url}`).join(' · ') || undefined}>
            <div className="flex justify-end">
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() =>
                  downloadBlob(new Blob([prettyJson(result.request_body)], { type: 'application/json' }), `fbr-test-${result.environment}.json`)
                }
              >
                <Download className="mr-1 h-4 w-4" /> Download JSON
              </Button>
            </div>
            <Pre text={prettyJson(result.request_body)} />
          </Section>
        )}

        {result.invoice_number && (
          <Section title="FBR invoice number" hint="Encoded in the QR exactly as the receipt prints it.">
            <p className="tabular text-base font-bold">{result.invoice_number}</p>
            {qr && <img src={qr} alt={`QR for ${result.invoice_number}`} className="h-40 w-40" />}
          </Section>
        )}

        {result.steps.map((s) => (
          <Section key={s.phase} title={`Gateway response — ${s.phase}`} hint={s.http_status ? `HTTP ${s.http_status} · status ${s.status_code || '—'}` : undefined}>
            {s.error_code && (
              <p className="text-sm text-destructive">
                {s.error_code}: {s.error}
                {s.line > 0 ? ` (line ${s.line})` : ''}
              </p>
            )}
            <Pre text={prettyJson(s.raw_response)} />
          </Section>
        ))}
      </DialogContent>
    </Dialog>
  )
}
