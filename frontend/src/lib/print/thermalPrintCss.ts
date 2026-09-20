/**
 * Shared base CSS for every thermal slip (receipt, Z-report). Ported from the
 * retail POS.
 *
 * Important: the `@page` rule MUST declare an explicit width in mm
 * (`size: 80mm auto`, never `size: auto`). Chromium's print dialog filters the
 * printer list by the document's declared paper size — with `auto` it falls
 * back to the OS default (A4) and thermal printers either vanish from the
 * dropdown or the driver pads every job out to A4 length.
 *
 * `lockExactPageSize` in `printPageSize.ts` later appends a SECOND `@page`
 * rule carrying an explicit measured height (`size: 80mm 47mm`) so the driver
 * feeds only what the slip needs. The later rule wins the cascade, so the auto
 * height is replaced — but the printer-list step has already used the width
 * declared here, which is exactly why the explicit width matters.
 */
export interface ThermalCssOptions {
  /** Physical roll width in mm — the load-bearing value, declared to the driver. */
  paperWidthMm: number
  /** True printable area in mm — the width of the visible content frame. */
  printableAreaMm: number
  /** Uniform scale applied to every font-size below. */
  fontScale: number
}

/** 58mm paper carries about a third fewer characters per line than 80mm. */
export function fontScaleFor(paperWidthMm: number): number {
  return paperWidthMm <= 58 ? 0.86 : 1
}

export function thermalPrintBaseCss(opts: ThermalCssOptions): string {
  const paper = clamp(opts.paperWidthMm, 40, 112)
  const printable = clamp(opts.printableAreaMm, 40, paper)
  const s = Number.isFinite(opts.fontScale) && opts.fontScale > 0 ? opts.fontScale : 1
  const pt = (n: number) => (n * s).toFixed(2) + 'pt'
  return [
    '@page { size: ' + paper + 'mm auto; margin: 0; }',
    '* { box-sizing: border-box; }',
    'html, body { margin: 0; padding: 0; background: #fff; color: #000;',
    '  width: ' + paper + 'mm; max-width: ' + paper + 'mm;',
    '  font-family: "Segoe UI", Arial, Helvetica, sans-serif;',
    '  font-size: ' + pt(9) + '; line-height: 1.32;',
    '  -webkit-print-color-adjust: exact; print-color-adjust: exact; }',
    '.thermal-page { position: relative; width: ' + printable + 'mm; max-width: ' + printable + 'mm;',
    '  padding: 2mm 0 6mm; margin: 0 auto; }',
    '.center { text-align: center; }',
    '.right { text-align: right; }',
    '.bold { font-weight: 700; }',
    '.rule { border-top: 1px dashed #000; margin: 1.6mm 0; }',
    '.rule-solid { border-top: 1px solid #000; margin: 1.6mm 0; }',
    '.logo { max-width: 60%; max-height: 18mm; display: block; margin: 0 auto 1.2mm; }',
    '.biz { font-size: ' + pt(13) + '; font-weight: 700; text-transform: uppercase; }',
    '.meta { font-size: ' + pt(8) + '; }',
    '.title { font-size: ' + pt(12) + '; font-weight: 700; letter-spacing: 0.6mm; text-align: center; }',
    'table { width: 100%; border-collapse: collapse; }',
    'td { vertical-align: top; padding: 0; }',
    '.kv td { font-size: ' + pt(8.5) + '; }',
    '.kv td.k { white-space: nowrap; padding-right: 2mm; }',
    '.kv td.v { text-align: right; }',
    '.items td { font-size: ' + pt(9) + '; }',
    '.items .name { font-weight: 700; padding-top: 1mm; }',
    '.items .qty { font-size: ' + pt(8.5) + '; }',
    '.items .sub { font-size: ' + pt(7.5) + '; padding-left: 2mm; }',
    '.items .amt { text-align: right; white-space: nowrap; }',
    '.totals td { font-size: ' + pt(9) + '; }',
    '.totals td.v { text-align: right; white-space: nowrap; }',
    '.grand td { font-size: ' + pt(13) + '; font-weight: 700; padding: 1.2mm 0; }',
    '.foot { font-size: ' + pt(8) + '; text-align: center; }',
    '.void-stamp { position: absolute; top: 34%; left: 0; right: 0; text-align: center;',
    '  font-size: ' + pt(28) + '; font-weight: 900; letter-spacing: 1.5mm;',
    '  border: 1mm solid #000; padding: 1mm 0; transform: rotate(-16deg);',
    '  opacity: 0.32; pointer-events: none; }',
  ].join('\n')
}

function clamp(n: number, lo: number, hi: number): number {
  if (!Number.isFinite(n)) return lo
  return Math.max(lo, Math.min(hi, n))
}
