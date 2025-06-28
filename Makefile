.PHONY: build test clean install docker

# Variables
BINARY_NAME=hyp
MAIN_PATH=cmd/hyp
VERSION?=0.1.0
BUILD_TIME=$(shell date +%FT%T%z)
LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

# Build
build:
	go build ${LDFLAGS} -o bin/${BINARY_NAME} ./${MAIN_PATH}

# Install
install:
	go install ${LDFLAGS} ./${MAIN_PATH}

# Test
test:
	go test -v ./...

# Test with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean
clean:
	go clean
	rm -rf bin/
	rm -f coverage.out coverage.html

# Run
run:
	go run ./${MAIN_PATH}

# Docker build
docker:
	docker build -t hypgo:${VERSION} .

# Lint
lint:
	golangci-lint run

# Format
fmt:
	go fmt ./...

# Vet
vet:
	go vet ./...

# Download dependencies
deps:
	go mod download
	go mod tidy

# Release build
release:
	GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-linux-amd64 ./${MAIN_PATH}
	GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-darwin-amd64 ./${MAIN_PATH}
	GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY_NAME}-windows-amd64.exe ./${MAIN_PATH}