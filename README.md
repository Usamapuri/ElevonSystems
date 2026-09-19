# Elevon POS

Point-of-sale for an LPG supplier: weight-based till, customer credit ledger, day close, reports, FBR Digital Invoicing.

- Design spec: `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md`
- Plans: `docs/superpowers/plans/`
- Local dev: `cp .env.example .env`, set `INITIAL_ADMIN_PASSWORD`, then `docker compose -f docker-compose.dev.yml up`. Frontend http://localhost:3000, API http://localhost:8080/health.
- Tests: `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run type-check && npm run test`.
