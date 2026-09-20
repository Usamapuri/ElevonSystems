# Elevon POS

Point-of-sale for an LPG supplier: weight-based till, customer credit ledger, day close, reports, FBR Digital Invoicing.

- Design spec: `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md`
- Plans: `docs/superpowers/plans/`
- Local dev: `cp .env.example .env`, set `INITIAL_ADMIN_PASSWORD`, then `docker compose -f docker-compose.dev.yml up`. Frontend http://localhost:3000, API http://localhost:8080/health.
- Tests: `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run type-check && npm run test`.

## Printing

The POS builds every document — thermal receipt, A4 tax invoice, Z-report — as a finished HTML string and hands it to one transport (`frontend/src/lib/print/transport.ts`). In a browser the HTML goes into an off-screen iframe with the `@page` size locked to the roll width, and `window.print()` fires there. Paper width and printable area come from Settings → Receipt (`receipt_paper_width_mm`, `receipt_printable_area_mm`).

**Silent printing.** A browser raises the print dialog on every job, which is unusable on a till. Chrome and Edge skip it when started with `--kiosk-printing`, and then print to the Windows **default** printer — so set the thermal printer as the default and launch the POS with:

```
frontend/public/kiosk-print.cmd                        # http://localhost:3000
frontend/public/kiosk-print.cmd https://pos.example    # a deployed store
```

The script is served at `/kiosk-print.cmd` on the deployed frontend, so a new till can download it from the POS itself. Put a shortcut to it in `shell:startup` to have the till come up printing silently after a reboot. Per-document printer routing (receipt → thermal, A4 → office laser) needs the Phase 8 desktop app, which prints through the `window.elevon` bridge the same transport already feature-detects.

**Debugging a print.** Chrome's "Print Failed" dialog says nothing useful. Every step of the pipeline traces to the console; set `localStorage.print_debug = 'v'` in DevTools on the till for stack traces, `'0'` to silence it.
