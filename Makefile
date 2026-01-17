# Makefile for CLIProxyAPI

# Variables
APP_NAME := CLIProxyAPI
BINARY_NAME := cliproxy-server
DOCKER_IMAGE := eceasy/cli-proxy-api
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X 'main.Version=$(VERSION)' -X 'main.Commit=$(COMMIT)' -X 'main.BuildDate=$(BUILD_DATE)'

# Go build settings
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOCLEAN := $(GOCMD) clean
GOGET := $(GOCMD) get

# Build targets
BUILD_DIR := build
CMD_DIR := cmd/server

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

.PHONY: all build clean test deps docker docker-build docker-push docker-build-push help

# Default target
all: help

## help: Show this help message
help:
	@echo "$(BLUE)CLIProxyAPI Makefile$(NC)"
	@echo ""
	@echo "$(GREEN)Available targets:$(NC)"
	@echo "  $(YELLOW)build$(NC)              - Build the application binary"
	@echo "  $(YELLOW)build-linux$(NC)        - Build Linux binary (amd64)"
	@echo "  $(YELLOW)build-darwin$(NC)       - Build macOS binary (arm64)"
	@echo "  $(YELLOW)build-windows$(NC)      - Build Windows binary (amd64)"
	@echo "  $(YELLOW)build-all$(NC)          - Build binaries for all platforms"
	@echo "  $(YELLOW)clean$(NC)              - Clean build artifacts"
	@echo "  $(YELLOW)test$(NC)               - Run all tests"
	@echo "  $(YELLOW)test-verbose$(NC)       - Run tests with verbose output"
	@echo "  $(YELLOW)deps$(NC)               - Download dependencies"
	@echo "  $(YELLOW)tidy$(NC)               - Tidy dependencies"
	@echo "  $(YELLOW)docker-build$(NC)       - Build Docker image"
	@echo "  $(YELLOW)docker-push$(NC)        - Push Docker image to registry"
	@echo "  $(YELLOW)docker-build-push$(NC)  - Build and push Docker image"
	@echo "  $(YELLOW)docker-run$(NC)         - Run application in Docker"
	@echo "  $(YELLOW)docker-compose-up$(NC)  - Start services with docker-compose"
	@echo "  $(YELLOW)docker-compose-down$(NC) - Stop docker-compose services"
	@echo "  $(YELLOW)run$(NC)                - Run the application locally"
	@echo "  $(YELLOW)version$(NC)            - Show version information"
	@echo ""

## version: Show version information
version:
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

## deps: Download dependencies
deps:
	@echo "$(BLUE)Downloading dependencies...$(NC)"
	$(GOMOD) download

## tidy: Tidy dependencies
tidy:
	@echo "$(BLUE)Tidying dependencies...$(NC)"
	$(GOMOD) tidy

## build: Build the application binary
build:
	@echo "$(BLUE)Building $(BINARY_NAME)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY_NAME)$(NC)"

## build-linux: Build Linux binary (amd64)
build-linux:
	@echo "$(BLUE)Building $(BINARY_NAME) for Linux (amd64)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64$(NC)"

## build-darwin: Build macOS binary (arm64)
build-darwin:
	@echo "$(BLUE)Building $(BINARY_NAME) for macOS (arm64)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64$(NC)"

## build-windows: Build Windows binary (amd64)
build-windows:
	@echo "$(BLUE)Building $(BINARY_NAME) for Windows (amd64)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe$(NC)"

## build-all: Build binaries for all platforms
build-all: build-linux build-darwin build-windows
	@echo "$(GREEN)All platform builds complete!$(NC)"

## clean: Clean build artifacts
clean:
	@echo "$(BLUE)Cleaning build artifacts...$(NC)"
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	@echo "$(GREEN)Clean complete$(NC)"

## test: Run all tests
test:
	@echo "$(BLUE)Running tests...$(NC)"
	$(GOTEST) ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "$(BLUE)Running tests with verbose output...$(NC)"
	$(GOTEST) -v ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "$(BLUE)Running tests with coverage...$(NC)"
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

## run: Run the application locally
run: build
	@echo "$(BLUE)Running $(BINARY_NAME)...$(NC)"
	./$(BUILD_DIR)/$(BINARY_NAME)

## docker-build: Build Docker image
docker-build:
	@echo "$(BLUE)Building Docker image $(DOCKER_IMAGE):$(VERSION)...$(NC)"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(VERSION) \
		-t $(DOCKER_IMAGE):latest \
		.
	@echo "$(GREEN)Docker image built successfully$(NC)"

## docker-push: Push Docker image to registry
docker-push:
	@echo "$(BLUE)Pushing Docker image $(DOCKER_IMAGE):$(VERSION)...$(NC)"
	docker push $(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_IMAGE):latest
	@echo "$(GREEN)Docker image pushed successfully$(NC)"

## docker-build-push: Build and push Docker image
docker-build-push: docker-build docker-push

## docker-run: Run application in Docker
docker-run:
	@echo "$(BLUE)Running Docker container...$(NC)"
	docker run -d \
		--name cli-proxy-api \
		-p 8317:8317 \
		-v $(PWD)/config.yaml:/CLIProxyAPI/config.yaml \
		-v $(PWD)/auths:/root/.cli-proxy-api \
		-v $(PWD)/logs:/CLIProxyAPI/logs \
		$(DOCKER_IMAGE):$(VERSION)
	@echo "$(GREEN)Container started: cli-proxy-api$(NC)"

## docker-stop: Stop Docker container
docker-stop:
	@echo "$(BLUE)Stopping Docker container...$(NC)"
	docker stop cli-proxy-api || true
	docker rm cli-proxy-api || true
	@echo "$(GREEN)Container stopped$(NC)"

## docker-logs: Show Docker container logs
docker-logs:
	docker logs -f cli-proxy-api

## docker-compose-up: Start services with docker-compose
docker-compose-up:
	@echo "$(BLUE)Starting docker-compose services...$(NC)"
	VERSION=$(VERSION) COMMIT=$(COMMIT) BUILD_DATE=$(BUILD_DATE) docker-compose up -d
	@echo "$(GREEN)Services started$(NC)"

## docker-compose-down: Stop docker-compose services
docker-compose-down:
	@echo "$(BLUE)Stopping docker-compose services...$(NC)"
	docker-compose down
	@echo "$(GREEN)Services stopped$(NC)"

## docker-compose-logs: Show docker-compose logs
docker-compose-logs:
	docker-compose logs -f

## docker-compose-build: Build docker-compose services
docker-compose-build:
	@echo "$(BLUE)Building docker-compose services...$(NC)"
	VERSION=$(VERSION) COMMIT=$(COMMIT) BUILD_DATE=$(BUILD_DATE) docker-compose build
	@echo "$(GREEN)Services built$(NC)"

## docker-compose-rebuild: Rebuild and restart docker-compose services
docker-compose-rebuild: docker-compose-down docker-compose-build docker-compose-up

## lint: Run linters (requires golangci-lint)
lint:
	@echo "$(BLUE)Running linters...$(NC)"
	@which golangci-lint > /dev/null || (echo "$(RED)golangci-lint not found. Install it from https://golangci-lint.run/$(NC)" && exit 1)
	golangci-lint run ./...

## fmt: Format code
fmt:
	@echo "$(BLUE)Formatting code...$(NC)"
	$(GOCMD) fmt ./...
	@echo "$(GREEN)Code formatted$(NC)"

## install-deps: Install required development tools
install-deps:
	@echo "$(BLUE)Installing development dependencies...$(NC)"
	$(GOGET) github.com/go-sql-driver/mysql@latest
	$(GOMOD) tidy
	@echo "$(GREEN)Dependencies installed$(NC)"
