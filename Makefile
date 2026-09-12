.PHONY: fmt format format-check vet test test-race check verify web web-typecheck web-lint web-test web-build web-e2e build build-cli

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

format: fmt
	npm --prefix web run lint:fix

format-check:
	@files="$$(gofmt -l $$(find cmd internal -name '*.go' -type f))"; \
	if [ -n "$$files" ]; then \
		echo "Go files require gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

check: verify

verify: format-check vet test test-race web-typecheck web-lint web-test web-build web-e2e

web:
	npm --prefix web ci
	npm --prefix web run build

web-typecheck:
	npm --prefix web run typecheck

web-lint:
	npm --prefix web run lint

web-test:
	npm --prefix web test

web-build:
	npm --prefix web run build

web-e2e:
	npm --prefix web run test:e2e

build: web
	mkdir -p ./bin
	go build -tags webui -o ./bin/venuewire ./cmd/venuewire

build-cli:
	mkdir -p ./bin
	go build -o ./bin/venuewire ./cmd/venuewire
