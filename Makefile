.PHONY: build test clean install

# Variables
BINARY_NAME=hyp
MAIN_PATH=cmd/hyp
# 版本以 cmd/hyp/version.go 為單一來源，不以 -X main.Version 覆寫
BUILD_TIME=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS=-ldflags "-X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}"

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