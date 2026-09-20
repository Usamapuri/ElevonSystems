COMPOSE = docker compose -f docker-compose.dev.yml

.PHONY: help dev up down logs db-shell test test-backend test-frontend seed-demo

help:
	@echo "dev            start postgres + backend (air) + frontend (vite)"
	@echo "down           stop the stack"
	@echo "logs           tail all logs"
	@echo "db-shell       psql into the dev database"
	@echo "test           run backend and frontend suites"
	@echo "seed-demo      insert demo products/customers into the local dev DB (refuses if GIN_MODE=release)"

dev up:
	$(COMPOSE) up --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

db-shell:
	$(COMPOSE) exec postgres psql -U $${DB_USER:-postgres} -d $${DB_NAME:-elevon_pos}

test: test-backend test-frontend

test-backend:
	cd backend && go vet ./... && go test ./...

test-frontend:
	cd frontend && npm run type-check && npm run test

seed-demo:
	cd backend && go run ./cmd/seed-demo
