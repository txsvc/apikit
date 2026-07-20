VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
BUILD   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/txsvc/apikit.Version=$(VERSION) -X github.com/txsvc/apikit.Build=$(BUILD)"

.PHONY: build run reset clean

build:
	go build $(LDFLAGS) -o bin/server .

run:
	cd bin && ./server

reset:
	rm -rf bin/data
	mkdir -p bin/data
	cp config.toml bin/config.toml
	cd bin && ./server --admin-email=admin@example.com

clean:
	rm -f bin/server
