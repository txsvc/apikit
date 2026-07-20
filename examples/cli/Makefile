VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
BUILD   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/txsvc/apikit.Version=$(VERSION) -X github.com/txsvc/apikit.Build=$(BUILD)"

.PHONY: build clean

build:
	go build $(LDFLAGS) -o bin/mycli .

clean:
	rm -f bin/mycli
