.PHONY: all build build-cli build-server test test-race vet tidy clean docker-build

all: tidy vet test build

build: build-cli build-server

build-cli:
	@mkdir -p bin
	go build -trimpath -o bin/envil ./cmd/envil

build-server:
	@mkdir -p bin
	go build -trimpath -o bin/envil-server ./cmd/envil-server

test:
	go test -v ./...

test-race:
	go test -v -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/ coverage.out

docker-build:
	docker build -t envil-server:latest .
