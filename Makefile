.PHONY: fmt vet test check web build build-cli

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

vet:
	go vet ./...

test:
	go test ./...

check: fmt vet test

web:
	npm --prefix web ci
	npm --prefix web run build

build: web
	mkdir -p ./bin
	go build -tags webui -o ./bin/venuewire ./cmd/venuewire

build-cli:
	mkdir -p ./bin
	go build -o ./bin/venuewire ./cmd/venuewire
