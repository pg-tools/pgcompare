.PHONY: build test lint fmt cover clean

build:
	go build -o pgcompare .

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	golangci-lint run --fix

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -f pgcompare coverage.out coverage.html
