/**
 * Debug logger for the print pipeline (ported from the retail POS).
 *
 * Why this exists: Chrome's native "Print Failed — Something went wrong when
 * trying to print" dialog gives zero diagnostic information. Every print job
 * here shares one off-screen-iframe + `win.print()` + `setTimeout(cleanup)`
 * pattern and swallows errors silently, so without a trace the only signal a
 * cashier can give you is "it didn't print". This module gives every step a
 * console line so the exact failure point is visible in DevTools on the till.
 *
 * Toggle: on by default. `localStorage.print_debug = '0'` silences it;
 * `localStorage.print_debug = 'v'` adds stack traces to caught errors.
 *
 * Purely observational — it never changes what is printed.
 */

type Level = 'info' | 'warn' | 'error'

function enabled(): boolean {
  try {
    return localStorage.getItem('print_debug') !== '0'
  } catch {
    return true
  }
}

function verbose(): boolean {
  try {
    return localStorage.getItem('print_debug') === 'v'
  } catch {
    return false
  }
}

function stamp(): string {
  const d = new Date()
  const pad = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

function emit(level: Level, tag: string, msg: string, data?: unknown): void {
  if (!enabled()) return
  const args: unknown[] = [`[print:${tag}] ${stamp()} ${msg}`]
  if (data !== undefined) args.push(data)
  const sink = console[level === 'info' ? 'log' : level] as (...a: unknown[]) => void
  sink(...args)
}

function now(): number {
  return typeof performance !== 'undefined' ? performance.now() : Date.now()
}

export const printDebug = {
  log(tag: string, msg: string, data?: unknown): void {
    emit('info', tag, msg, data)
  },
  warn(tag: string, msg: string, data?: unknown): void {
    emit('warn', tag, msg, data)
  },
  error(tag: string, msg: string, err: unknown): void {
    const payload =
      err instanceof Error
        ? { name: err.name, message: err.message, stack: verbose() ? err.stack : undefined }
        : err
    emit('error', tag, msg, payload)
  },
  /** Wraps a synchronous step with timing; rethrows after logging. */
  span<T>(tag: string, label: string, fn: () => T): T {
    if (!enabled()) return fn()
    const t0 = now()
    emit('info', tag, `> ${label}`)
    try {
      const out = fn()
      emit('info', tag, `ok ${label} (${(now() - t0).toFixed(1)}ms)`)
      return out
    } catch (err) {
      emit('error', tag, `fail ${label} (${(now() - t0).toFixed(1)}ms)`, err)
      throw err
    }
  },
  /** Wraps an async step with timing; rethrows after logging. */
  async spanAsync<T>(tag: string, label: string, fn: () => Promise<T>): Promise<T> {
    if (!enabled()) return fn()
    const t0 = now()
    emit('info', tag, `> ${label}`)
    try {
      const out = await fn()
      emit('info', tag, `ok ${label} (${(now() - t0).toFixed(1)}ms)`)
      return out
    } catch (err) {
      emit('error', tag, `fail ${label} (${(now() - t0).toFixed(1)}ms)`, err)
      throw err
    }
  },
  /**
   * Attaches beforeprint / afterprint / error listeners to a print iframe so
   * you can see whether Chrome actually started and finished the job. This is
   * what diagnoses the "iframe removed before Chrome captured the document"
   * race that produces the native "Print Failed" dialog.
   */
  attachWindowProbes(tag: string, win: Window, doc: Document): void {
    if (!enabled()) return
    const t0 = now()
    const elapsed = () => `${(now() - t0).toFixed(1)}ms`
    try {
      win.addEventListener('beforeprint', () => emit('info', tag, `beforeprint fired (${elapsed()})`))
      win.addEventListener('afterprint', () => emit('info', tag, `afterprint fired (${elapsed()})`))
      win.addEventListener('error', (ev) => {
        emit('error', tag, 'window error inside print iframe', {
          message: ev.message,
          filename: ev.filename,
          lineno: ev.lineno,
        })
      })
      doc.addEventListener('readystatechange', () => emit('info', tag, `doc.readyState -> ${doc.readyState}`))
    } catch (err) {
      emit('warn', tag, 'failed to attach window probes', err)
    }
  },
  /** One-line state dump, taken right before win.print(). */
  snapshot(tag: string, label: string, doc: Document | null, win: Window | null): void {
    if (!enabled()) return
    if (!doc || !win) {
      emit('warn', tag, `${label}: doc/win is null`, { doc: !!doc, win: !!win })
      return
    }
    emit('info', tag, label, {
      readyState: doc.readyState,
      title: doc.title,
      bodyChildren: doc.body?.children.length ?? 0,
      bodyScrollHeight: doc.body?.scrollHeight ?? 0,
      images: doc.images.length,
      imagesComplete: Array.from(doc.images).filter((i) => i.complete).length,
      stylesheets: doc.styleSheets.length,
    })
  },
}
