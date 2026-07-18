VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
BUILD   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")

LDFLAGS := -ldflags "-X github.com/txsvc/apikit.Version=$(VERSION) -X github.com/txsvc/apikit.Build=$(BUILD)"

.PHONY: build test lint check check-spec clean

build:
	go build -o bin/apikit $(LDFLAGS) ./cmd/apikit

test:
	go test ./... -count=1

lint:
	go vet ./...

check-spec:
	go run github.com/pb33f/libopenapi-validator/cmd/validate@latest api/openapi.yaml

check: lint test check-spec

clean:
	rm -f bin/apikit
