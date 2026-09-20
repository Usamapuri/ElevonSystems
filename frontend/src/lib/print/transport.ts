/**
 * The single seam between the web app and a printer.
 *
 * Every printable document in the POS — thermal receipt, A4 tax invoice,
 * Z-report — is built as a finished HTML string by a pure builder and handed
 * to `printHtml()` here. There is deliberately no second path: the retail
 * POS's `printTransport.ts` was merged into this file rather than copied
 * beside it, so there is exactly one place that decides how a job reaches
 * paper.
 *
 * Two transports:
 *   - Inside the Elevon desktop app (Phase 8), `window.elevon.print()` prints
 *     silently, optionally to a named printer.
 *   - In a browser, the HTML goes into an off-screen iframe and `print()`
 *     fires there, so no popup window steals focus from the till mid-service.
 *     Silent printing then needs Chrome/Edge launched with `--kiosk-printing`
 *     (see `public/kiosk-print.cmd`).
 *
 * Callers feature-detect through `desktop()`/`desktopPrint()`, so the same
 * bundle runs in both with no build-time branching.
 */
import { lockExactPageSize } from '@/lib/print/printPageSize'
import { printDebug } from '@/lib/print/printDebug'

export interface DesktopPrintResult {
  ok: boolean
  error?: string
}

export interface DesktopBridge {
  isDesktop: true
  appVersion(): Promise<string>
  print(req: { html: string; deviceName?: string; paperWidthMm?: number; printableAreaMm?: number }): Promise<DesktopPrintResult>
  getPrinters(): Promise<{ name: string; isDefault: boolean }[]>
  minimize(): Promise<void>
}

declare global {
  interface Window {
    elevon?: DesktopBridge
  }
}

/** The Electron bridge when running inside the desktop app, otherwise null. */
export function desktop(): DesktopBridge | null {
  if (typeof window === 'undefined') return null
  const b = window.elevon
  return b && b.isDesktop ? b : null
}

/**
 * Print finished HTML silently via the desktop app. Returns true if the bridge
 * took the job; false means "not in the desktop app, use window.print()".
 */
export function desktopPrint(
  html: string,
  opts: { deviceName?: string; paperWidthMm?: number; printableAreaMm?: number } = {},
): boolean {
  const d = desktop()
  if (!d) return false
  d.print({ html, deviceName: opts.deviceName || undefined, paperWidthMm: opts.paperWidthMm, printableAreaMm: opts.printableAreaMm })
    .then((r) => {
      if (!r.ok) printDebug.error('desktop', 'silent print failed', r.error)
    })
    .catch((e) => printDebug.error('desktop', 'silent print threw', e))
  return true
}

/**
 * The three page geometries the POS prints. `'80mm'`/`'58mm'` are thermal roll
 * widths (the builders read the real width from `receipt_paper_width_mm`);
 * `'a4'` is the full-page tax invoice.
 */
export type PageSize = '80mm' | '58mm' | 'a4'

export interface PrintHtmlOptions {
  pageSize: PageSize
  /** Visible content width for a thermal job; ignored for A4. */
  printableAreaMm?: number
  /** Target a specific printer when running inside the desktop app. */
  deviceName?: string
}

/** '80mm' -> 80. A4 has no roll width. */
export function paperWidthFor(pageSize: PageSize): number | undefined {
  if (pageSize === '80mm') return 80
  if (pageSize === '58mm') return 58
  return undefined
}

/** The nearest supported roll width for a settings value. */
export function pageSizeForWidth(widthMm: number): PageSize {
  return widthMm <= 58 ? '58mm' : '80mm'
}

/**
 * Send finished HTML to a printer. Resolves once the job has been handed off
 * (the desktop bridge accepted it, or `print()` has fired in the iframe) —
 * never rejects, because a failed receipt must not roll back a recorded sale.
 */
export async function printHtml(html: string, opts: PrintHtmlOptions): Promise<void> {
  const paperWidthMm = paperWidthFor(opts.pageSize)
  if (desktopPrint(html, { deviceName: opts.deviceName, paperWidthMm, printableAreaMm: opts.printableAreaMm })) return
  if (typeof document === 'undefined') {
    printDebug.warn('transport', 'no document — print skipped (non-browser environment)')
    return
  }
  await printViaOffscreenIframe(html, paperWidthMm)
}

/**
 * Off-screen iframe + print(). The iframe is tall (12000px) so the whole slip
 * lays out in one go and `scrollHeight` measures the real content height, and
 * it is parked far off-screen rather than hidden: a `display:none` iframe has
 * no layout, so it would measure zero.
 */
function printViaOffscreenIframe(html: string, paperWidthMm: number | undefined): Promise<void> {
  const tag = 'iframe'
  return new Promise<void>((resolve) => {
    const iframe = document.createElement('iframe')
    iframe.setAttribute(
      'style',
      ['position:fixed', 'left:-8000px', 'top:0', 'width:320px', 'height:12000px', 'border:0', 'opacity:0', 'pointer-events:none', 'z-index:-1'].join(';'),
    )
    iframe.setAttribute('aria-hidden', 'true')
    document.body.appendChild(iframe)

    const doc = iframe.contentDocument
    const win = iframe.contentWindow
    if (!doc || !win) {
      printDebug.warn(tag, 'contentDocument/contentWindow is null — aborting')
      try {
        document.body.removeChild(iframe)
      } catch {
        /* already gone */
      }
      resolve()
      return
    }

    doc.open()
    doc.write(html)
    doc.close()
    printDebug.attachWindowProbes(tag, win, doc)

    const run = async () => {
      try {
        if (paperWidthMm !== undefined) {
          await printDebug.spanAsync(tag, 'lockExactPageSize', () => lockExactPageSize(doc, paperWidthMm))
        }
        printDebug.snapshot(tag, 'pre-print snapshot', doc, win)
        win.focus()
        printDebug.span(tag, 'win.print()', () => win.print())
      } catch (err) {
        printDebug.error(tag, 'print run threw', err)
      } finally {
        // Chrome captures the document asynchronously; removing the iframe too
        // early is what produces its useless "Print Failed" dialog.
        setTimeout(() => {
          try {
            document.body.removeChild(iframe)
          } catch (err) {
            printDebug.warn(tag, 'iframe removal failed', err)
          }
          resolve()
        }, 800)
      }
    }

    requestAnimationFrame(() => {
      setTimeout(() => {
        void run()
      }, 300)
    })
  })
}
