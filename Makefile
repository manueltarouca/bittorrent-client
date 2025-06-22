# BitTorrent Client Makefile

# Build variables
BINARY_NAME=bittorrent
CMD_PATH=cmd/bittorrent/main.go
BUILD_DIR=build
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

# Go variables
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)

.PHONY: all build clean test run help install deps fmt vet lint check

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) $(CMD_PATH)

# Build for multiple platforms
build-all: build-linux build-windows build-darwin

build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_PATH)

build-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(CMD_PATH)

build-darwin:
	@echo "Building for macOS..."
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(CMD_PATH)

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod tidy
	go mod download

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Run golint (if available)
lint:
	@echo "Running golint..."
	@if command -v golint >/dev/null 2>&1; then \
		golint ./...; \
	else \
		echo "golint not installed, skipping..."; \
	fi

# Run staticcheck (if available)
staticcheck:
	@echo "Running staticcheck..."
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed, skipping..."; \
	fi

# Run all checks
check: fmt vet lint staticcheck test

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME).exe
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -rf BigBuckBunny_124/
	rm -f *.iso *.avi *.mp4 *.ogv *.gif

# Run the binary with example torrent
run: build
	@echo "Running $(BINARY_NAME) with example torrent..."
	./$(BINARY_NAME) -t example/BigBuckBunny_124_archive.torrent -v

# Show torrent info
info: build
	@echo "Showing torrent info..."
	./$(BINARY_NAME) --info -t example/BigBuckBunny_124_archive.torrent

# Test tracker connectivity
announce: build
	@echo "Testing tracker connectivity..."
	./$(BINARY_NAME) -t example/BigBuckBunny_124_archive.torrent --announce-only -v

# Download to specific directory
download: build
	@echo "Downloading to downloads/ directory..."
	mkdir -p downloads
	./$(BINARY_NAME) -t example/BigBuckBunny_124_archive.torrent -o downloads -v

# Install the binary to GOPATH/bin
install: build
	@echo "Installing $(BINARY_NAME) to $(GOPATH)/bin..."
	cp $(BINARY_NAME) $(GOPATH)/bin/

# Create release archives
release: build-all
	@echo "Creating release archives..."
	mkdir -p $(BUILD_DIR)/release
	tar -czf $(BUILD_DIR)/release/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-linux-amd64
	zip -j $(BUILD_DIR)/release/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe
	tar -czf $(BUILD_DIR)/release/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-darwin-amd64

# Initialize development environment
dev-setup:
	@echo "Setting up development environment..."
	go mod init github.com/yourusername/bittorrent-client 2>/dev/null || true
	go mod tidy
	@echo "Installing development tools..."
	go install golang.org/x/lint/golint@latest 2>/dev/null || echo "Failed to install golint"
	go install honnef.co/go/tools/cmd/staticcheck@latest 2>/dev/null || echo "Failed to install staticcheck"

# Show help
help:
	@echo "BitTorrent Client Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  build        - Build the binary"
	@echo "  build-all    - Build for all platforms (linux, windows, darwin)"
	@echo "  clean        - Clean build artifacts and downloaded files"
	@echo "  test         - Run tests"
	@echo "  test-coverage- Run tests with coverage report"
	@echo "  fmt          - Format code"
	@echo "  vet          - Run go vet"
	@echo "  lint         - Run golint"
	@echo "  staticcheck  - Run staticcheck"
	@echo "  check        - Run all checks (fmt, vet, lint, staticcheck, test)"
	@echo "  run          - Build and run with example torrent"
	@echo "  info         - Show example torrent information"
	@echo "  announce     - Test tracker connectivity"
	@echo "  download     - Download example torrent to downloads/ directory"
	@echo "  install      - Install binary to GOPATH/bin"
	@echo "  release      - Create release archives for all platforms"
	@echo "  dev-setup    - Set up development environment"
	@echo "  deps         - Install dependencies"
	@echo "  help         - Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  make build                    # Build the binary"
	@echo "  make run                      # Build and run with example torrent"
	@echo "  make download                 # Download example torrent"
	@echo "  make check                    # Run all code quality checks"
	@echo "  make release VERSION=v1.0.0  # Create release with specific version"