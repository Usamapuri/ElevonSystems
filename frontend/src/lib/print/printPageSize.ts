/**
 * Page-size lock for thermal print jobs. Ported from the retail POS.
 *
 * Problem: the slip declares `@page { size: 80mm auto }`, but Chromium on
 * Windows ignores `auto` when the selected printer has a fixed paper-size
 * preset baked into its driver (most 80mm thermal drivers ship an
 * "80 x 297 mm" preset). The preset wins and the printer feeds the full
 * 297mm however short the slip is — most of a page of blank roll per sale.
 *
 * Fix: before `window.print()`, measure the rendered body height inside the
 * iframe, convert to millimetres at the 96 DPI CSS reference, and append a
 * final `@page` rule with an EXPLICIT size (`size: 80mm 47mm`). Chromium
 * honours explicit dimensions where it falls back on `auto`, so the print
 * system sends a custom page size to the driver and it feeds and cuts to
 * length.
 *
 * Call this AFTER the iframe document has been written but BEFORE print()
 * fires. It is async because it waits on fonts and images: measuring before
 * either has settled over- or under-shoots the real content height.
 */
import { printDebug } from '@/lib/print/printDebug'

const PX_PER_MM = 96 / 25.4 // 3.7795 — the CSS reference DPI

export interface LockPageSizeOptions {
  /** Top + bottom @page margin in mm. Default 4. */
  topBottomMarginMm?: number
  /** Left + right @page margin in mm. Default 0 — the slip already pads itself. */
  leftRightMarginMm?: number
  /**
   * Blank mm appended below the last line. Must exceed the printer's physical
   * printhead-to-cutter distance (~13.5mm on common 80mm heads) so the cutter
   * engages on blank paper rather than mid-total. Default 20.
   */
  trailingMarginMm?: number
}

/**
 * Waits for fonts and images, measures the document, and appends an explicit
 * `@page` rule so the job produces a page exactly as tall as the content plus
 * a tear-off margin. Resolves once the override is in the DOM; the caller then
 * fires print().
 */
export async function lockExactPageSize(
  doc: Document,
  paperWidthMm: number,
  options: LockPageSizeOptions = {},
): Promise<void> {
  const tag = 'pagesize'
  const topBottom = options.topBottomMarginMm ?? 4
  const leftRight = options.leftRightMarginMm ?? 0
  const trailing = options.trailingMarginMm ?? 20

  // Pin the document to the exact printable width before measuring, so the
  // content lays out at the width it will print at. The host iframe is about
  // 320px (~85mm), which would otherwise let lines run wider than the paper
  // and under-measure the wrapped height.
  applyMeasurementWidth(doc, paperWidthMm - leftRight * 2)
  await waitForLayoutReady(doc)

  const heightPx = Math.max(doc.body?.scrollHeight ?? 0, doc.documentElement?.scrollHeight ?? 0)
  if (heightPx <= 0) {
    printDebug.warn(tag, 'measured heightPx <= 0 — skipping @page injection')
    return
  }

  // scrollHeight already includes the slip's own padding; only the @page
  // margin (which sits outside the body box) and the tear-off need adding.
  const heightMm = heightPx / PX_PER_MM + topBottom * 2 + trailing
  const finalHeightMm = Math.max(20, Math.ceil(heightMm))

  const style = doc.createElement('style')
  style.setAttribute('data-page-size-lock', 'true')
  style.textContent =
    '@page { size: ' + paperWidthMm + 'mm ' + finalHeightMm + 'mm; margin: ' + topBottom + 'mm ' + leftRight + 'mm; }'
  // Appended last so it wins the cascade against the slip's own @page rule.
  doc.head.appendChild(style)
  printDebug.log(tag, '@page rule injected', { paperWidthMm, finalHeightMm, heightPx })
}

function applyMeasurementWidth(doc: Document, widthMm: number): void {
  const sheet = doc.createElement('style')
  sheet.setAttribute('data-page-size-lock-measure', 'true')
  // !important so the slip's own width rules do not win.
  sheet.textContent =
    'html, body { width: ' + widthMm + 'mm !important; max-width: ' + widthMm + 'mm !important; box-sizing: border-box !important; }'
  doc.head.appendChild(sheet)
}

async function waitForLayoutReady(doc: Document): Promise<void> {
  const tag = 'pagesize'
  const win = doc.defaultView

  const fonts = (doc as Document & { fonts?: { ready?: Promise<unknown> } }).fonts
  if (fonts?.ready) {
    try {
      await fonts.ready
    } catch (err) {
      printDebug.warn(tag, 'fonts.ready rejected — proceeding anyway', err)
    }
  }

  const images = Array.from(doc.images)
  if (images.length > 0) {
    await Promise.all(
      images.map(
        (img) =>
          new Promise<void>((resolve) => {
            if (img.complete) {
              resolve()
              return
            }
            img.addEventListener('load', () => resolve(), { once: true })
            img.addEventListener(
              'error',
              () => {
                printDebug.warn(tag, 'image failed to load', { src: img.src })
                resolve()
              },
              { once: true },
            )
          }),
      ),
    )
  }

  // Two frames so layout has flushed after fonts and images settled.
  if (win) {
    await new Promise<void>((resolve) => {
      win.requestAnimationFrame(() => win.requestAnimationFrame(() => resolve()))
    })
  }
}
