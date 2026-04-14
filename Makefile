.PHONY: build test test-integration lint fmt cover clean

VERSION ?= dev

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o pgcompare .

test:
	go test ./...

test-integration:
	go test -tags=integration ./tests/...

lint:
	golangci-lint run

fmt:
	golangci-lint run --fix

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -f pgcompare coverage.out coverage.html
