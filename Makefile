SHELL := /bin/sh

.PHONY: test test-prepare test-unit test-int test-release-acceptance test-e2e docs-guardrails guardrails-test check-plugin-contract fmt lint vet help-smoke

test: test-prepare test-unit test-int test-release-acceptance test-e2e

# Preparation gate: local-only checks that never contact Hermes/Discord or other external services.
test-prepare: fmt lint vet guardrails-test docs-guardrails help-smoke

fmt:
	@if command -v gofmt >/dev/null 2>&1 && [ -d ./cmd -o -d ./internal -o -d ./pkg ]; then \
		files=$$(find . -path './.git' -prune -o -name '*.go' -print); \
		if [ -n "$$files" ]; then test -z "$$(gofmt -l $$files)"; fi; \
	else \
		echo "fmt: no Go source tree yet; skipped"; \
	fi

lint:
	@if command -v golangci-lint >/dev/null 2>&1 && [ -f go.mod ]; then \
		golangci-lint run ./...; \
	else \
		echo "lint: golangci-lint/go.mod unavailable; skipped until Go scaffold exists"; \
	fi

vet:
	@if command -v go >/dev/null 2>&1 && [ -f go.mod ]; then \
		go vet ./...; \
	else \
		echo "vet: go/go.mod unavailable; skipped until Go scaffold exists"; \
	fi

docs-guardrails:
	@python3 scripts/guardrails.py

guardrails-test:
	@python3 scripts/guardrails_test.py

help-smoke:
	@if command -v go >/dev/null 2>&1 && [ -f go.mod ]; then \
		cli_help=$$(go run ./cmd/atn-control --help); \
		printf '%s\n' "$$cli_help" | grep -q '^atn-control$$'; \
		daemon_help=$$(go run ./cmd/atn-controld --help); \
		printf '%s\n' "$$daemon_help" | grep -q '^atn-controld$$'; \
	else \
		echo "help-smoke: go/go.mod unavailable; skipped until Go scaffold exists"; \
	fi

check-plugin-contract:
	@python3 scripts/check_plugin_contract.py

test-unit:
	@if command -v go >/dev/null 2>&1 && [ -f go.mod ]; then \
		go test ./... -run 'TestUnit|Unit' -count=1; \
	else \
		echo "test-unit: no Go scaffold yet; docs-only pass"; \
	fi

test-int:
	@if command -v go >/dev/null 2>&1 && [ -f go.mod ]; then \
		ATN_TEST_MODE=integration ATN_EXTERNAL=0 go test ./... -run 'TestIntegration|Integration' -count=1; \
	else \
		echo "test-int: no Go scaffold yet; docs-only pass"; \
	fi

test-release-acceptance:
	@if command -v go >/dev/null 2>&1 && [ -f go.mod ]; then \
		ATN_TEST_MODE=release ATN_EXTERNAL=0 go test ./internal/command -run 'TestReleaseAcceptance' -count=1; \
	else \
		echo "test-release-acceptance: no Go scaffold yet; docs-only pass"; \
	fi

test-e2e:
	@if command -v go >/dev/null 2>&1 && [ -f go.mod ]; then \
		ATN_TEST_MODE=e2e ATN_PROFILE_HOME="$${ATN_PROFILE_HOME:-$$(mktemp -d)}" ATN_DISCORD_TEST_TARGET="$${ATN_DISCORD_TEST_TARGET:-}" go test ./... -run 'TestE2E|E2E' -count=1; \
	else \
		echo "test-e2e: no Go scaffold yet; docs-only pass"; \
	fi
