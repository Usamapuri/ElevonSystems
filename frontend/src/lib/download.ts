/**
 * The client half of a report export (spec §6.8): the server streams a CSV
 * or `.xlsx` blob and names it itself, in `Content-Disposition:
 * attachment; filename="<report>_<from>_<to>.<ext>"` (exposed via CORS —
 * backend/main.go `ExposeHeaders`). Trusting that header rather than
 * building a filename from the request params here means the two can never
 * drift apart, and the download keeps working if the server ever changes
 * its naming.
 */

/**
 * Pulls the filename out of a `Content-Disposition` header value. Handles
 * both the quoted form the backend sends (`attachment;
 * filename="daily_2026-09-01_2026-09-20.csv"`) and a bare, unquoted one, in
 * case a proxy in front of the API ever rewrites it. Returns null for a
 * missing or unparsable header — the caller falls back to a name it builds
 * itself rather than failing the download outright.
 */
export function parseContentDispositionFilename(header: string | null | undefined): string | null {
  if (!header) return null
  const quoted = /filename="([^"]+)"/i.exec(header)
  if (quoted?.[1]) return quoted[1]
  const bare = /filename=([^;]+)/i.exec(header)
  if (bare?.[1]) return bare[1].trim()
  return null
}

/**
 * Triggers a browser "Save As" for an in-memory blob under a chosen
 * filename. DOM-only — this project's vitest suite runs in a node
 * environment (logic tests only), so this function is exercised by hand,
 * never by the test suite; keep it to exactly this one job so everything
 * decidable stays in `parseContentDispositionFilename` above.
 */
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  try {
    a.click()
  } finally {
    a.remove()
    // Revoked on the next tick, not in the same one as the click: Chrome
    // tolerates a synchronous revoke, but Firefox and older WebKit can
    // cancel the save because the URL is gone before the download thread
    // reads it, and the Phase 8 desktop shell is a third unknown. The
    // anchor still goes straight away — only the URL waits.
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}
