/**
 * Single seam between the web app and the Elevon desktop (Electron) shell.
 * In a browser, printing goes through an off-screen iframe + window.print().
 * Inside the desktop app, window.elevon prints silently to a named printer.
 * Callers feature-detect via desktop()/desktopPrint() so the same bundle runs
 * in both with no build-time branching. The Electron side lands in Phase 8.
 */
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
      if (!r.ok) console.error('[print] desktop print failed:', r.error)
    })
    .catch((e) => console.error('[print] desktop print threw:', e))
  return true
}
