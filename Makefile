TIME := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
VERSION :=$(shell git describe --tags --always)

# Main target to display usage information
all:
	@echo "** Build Instructions **"
	@echo "To build for a specific platform, use one of the following targets:"
	@echo "  make linux-pc    # Build for Linux PC (x86-64)"
	@echo "  make linux-rpi   # Build for Linux Raspberry Pi (ARM64)"
	@echo "  make windows     # Build for Windows (x86-64)"

# Target to build for Linux PC (x86-64)
linux-pc:
	@echo "Building for Linux PC (x86-64)..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags="-w -s" -o nrs cmd/nrs/main.go

# Target to build for Linux Raspberry Pi (ARM64)
linux-rpi:
	@echo "Building for Linux Raspberry Pi (ARM64)..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags="-w -s" -o nrs-arm64 cmd/nrs/main.go

# Target to build for Windows (x86-64)
windows:
	@echo "Building for Windows (x86-64)..."
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags="-w -s" -o nrs.exe cmd/nrs/main.go

docker:
	@echo "Building Docker image..."
	docker build -t gmouradev96/nrs --build-arg VERSION=$(VERSION) --build-arg BUILD_DATE=$(TIME) .

.PHONY: all linux-pc linux-rpi windows docker