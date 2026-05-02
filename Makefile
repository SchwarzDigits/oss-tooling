.PHONY: build test lint run-inventory clean tidy vet

BIN := bin/osstool

build:
	go build -o $(BIN) ./cmd/osstool

test:
	go test ./...

lint:
	golangci-lint run

vet:
	go vet ./...

tidy:
	go mod tidy

run-inventory:
	go run ./cmd/osstool inventory run

clean:
	rm -rf bin output
