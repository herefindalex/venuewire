.PHONY: fmt vet test check build

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

vet:
	go vet ./...

test:
	go test ./...

check: fmt vet test

build:
	mkdir -p ./bin
	go build -o ./bin/venuewire ./cmd/venuewire
