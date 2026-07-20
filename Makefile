VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
BUILD   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")

LDFLAGS := -ldflags "-X github.com/txsvc/apikit.Version=$(VERSION) -X github.com/txsvc/apikit.Build=$(BUILD) -X github.com/txsvc/apikit/internal/cli.Version=$(VERSION) -X github.com/txsvc/apikit/internal/cli.Build=$(BUILD)"

.PHONY: build test lint check check-spec clean

build:
	go build -o bin/apikit $(LDFLAGS) ./cmd/apikit
	go build -o bin/akc $(LDFLAGS) ./cmd/akc

test:
	go test ./... -count=1
	rm apikit db.test

lint:
	go vet ./...

check-spec:
	go run github.com/pb33f/libopenapi-validator/cmd/validate@latest api/openapi.yaml

check: lint test check-spec

clean:
	rm -f bin/apikit bin/akc

server-reset:
	rm -rf bin/data
	mkdir -p bin/data
	cd bin && ./apikit --admin-email=hello@micku.me

server-run:
	-mv bin/admin_token bin/token
	cd bin && ADMIN_TOKEN=$$(cat token) ./apikit 