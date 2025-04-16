.PHONY: build test clean lint

BINARY_NAME=repo
BINARY_WINDOWS=$(BINARY_NAME).exe
BINARY_LINUX=$(BINARY_NAME)
BINARY_MACOS=$(BINARY_NAME)

build:
	go build -o bin/$(BINARY_WINDOWS) ./cmd/repo

build-all: build-windows build-linux build-macos

build-windows:
	GOOS=windows GOARCH=amd64 go build -o bin/$(BINARY_WINDOWS) ./cmd/repo

build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY_LINUX) ./cmd/repo

build-macos:
	GOOS=darwin GOARCH=amd64 go build -o bin/$(BINARY_MACOS) ./cmd/repo

test:
	go test -v ./...

test-coverage:
	go test -coverprofile=coverage.txt -covermode=atomic ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/
	rm -f coverage.txt

run:
	go run ./cmd/repo/main.go