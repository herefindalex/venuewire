.PHONY: fmt vet test check build

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

vet:
	go vet ./...

test:
	go test ./...

check: fmt vet test

build:
	go build -o /bin/bybitctl ./cmd/bybitctl

