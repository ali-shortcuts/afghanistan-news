# Afghanistan News — developer entry points
SHELL := /usr/bin/env bash
API_BASE ?= http://localhost:8080
BACKEND := backend

.PHONY: preview-up preview-down preview-status help backend-build backend-test backend-run worker-run smoke bootstrap acceptance android-check verify preview \
        pg-up pg-status migrate-db backup restore stack stack-stop stack-status stack-logs parity

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

backend-build: ## compile the Go backend and both binaries
	cd $(BACKEND) && go build ./... && go vet ./...

backend-test: ## run the Go test suite
	cd $(BACKEND) && go test ./...

backend-run: ## run the API with the embedded worker (dev)
	cd $(BACKEND) && go run ./cmd/api

worker-run: ## run the ingestion worker as a separate process
	cd $(BACKEND) && go run ./cmd/worker

smoke: ## end-to-end health check against $(API_BASE)
	./scripts/smoke.sh

bootstrap: ## first-run: dry-run + commit the bundled feed pack, activate wave 1
	./scripts/bootstrap.sh

acceptance: ## full end-to-end check of every surface against $(API_BASE)
	./scripts/acceptance.sh

# ---------------------------------------------------------------- production shape (§Phase B)
preview-up: ## cold machine → PostgreSQL + content + api + worker, one command
	./scripts/preview.sh all

preview-down: ## stop api and worker
	./scripts/preview.sh stop

preview-status: ## process state, health, content counts, surface HTTP codes
	./scripts/preview.sh status

pg-up: ## install/start PostgreSQL and create the afnews role + database
	sudo ./scripts/bootstrap_postgres.sh

pg-status: ## postgres version, schema, content and index statistics
	./scripts/pg_status.sh

migrate-db: ## move data/afnews.db into PostgreSQL, then prove API parity
	./scripts/migrate_to_postgres.sh data/afnews.db

parity: ## compare the public API on both dialects, endpoint by endpoint
	./scripts/migrate_to_postgres.sh data/afnews.db --dry-run

backup: ## dump the database into ./backups and verify the dump is readable
	./scripts/backup.sh

restore: ## restore the newest dump into a scratch database (rehearsal)
	./scripts/restore.sh "$$(ls -t backups/*.dump | head -1)" --into afnews_restore --create

stack: ## run api + standalone worker against PostgreSQL, no Docker
	./scripts/run_production_stack.sh start

stack-stop: ## stop both processes
	./scripts/run_production_stack.sh stop

stack-status: ## show process state, health and article count
	./scripts/run_production_stack.sh status

stack-logs: ## tail the api log (make stack-logs WHICH=worker)
	./scripts/run_production_stack.sh logs 40 "$${WHICH:-api}"

android-check: ## static checks for the Android tree (no SDK needed)
	cd android && python3 tools/check_sources.py && python3 tools/check_deps_api21.py

android-build: ## build the debug APK for both ABIs + unit tests + lint (API 21 gate)
	cd android && JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
	  ./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug

verify: backend-build backend-test android-check acceptance ## everything, in order

verify-full: verify android-build pg-status ## everything plus the Android APK build

preview: ## print the local preview URLs
	@echo "landing        $(API_BASE)/"
	@echo "mobile client  $(API_BASE)/app/"
	@echo "admin console  $(API_BASE)/admin/"
	@echo "metrics        $(API_BASE)/metrics"
