# Makefile for cross-compiling Go binaries for multiple platforms and architectures

# Project name (used for binary naming)
PROJECT_NAME = livekit-ffmpeg

# Output directory for binaries
BIN_DIR = bin

# Go build flags
GOFLAGS = -ldflags="-s -w" -trimpath

# Source file
SRC = cmd/main.go

# Define OS-ARCH pairs
PLATFORMS = \
	darwin-amd64 \
	darwin-arm64 \
	linux-amd64 \
	linux-arm64

# Generate target list for all platforms
TARGETS = $(foreach plat,$(PLATFORMS),$(BIN_DIR)/$(PROJECT_NAME)-$(plat))

# Default target: build for all platforms
all: $(TARGETS)

# Define a template for build rules
define BUILD_RULE
$(BIN_DIR)/$(PROJECT_NAME)-$(1): $(SRC)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(word 1,$(subst -, ,$(1))) GOARCH=$(word 2,$(subst -, ,$(1))) go build $(GOFLAGS) -o $$@ $$<

.PHONY: $(1)
$(1): $(BIN_DIR)/$(PROJECT_NAME)-$(1)
endef

# Apply the build rule template to each platform
$(foreach plat,$(PLATFORMS),$(eval $(call BUILD_RULE,$(plat))))

# Clean up binaries
clean:
	rm -rf $(BIN_DIR)

# Help message
help:
	@echo "Makefile for cross-compiling $(PROJECT_NAME)"
	@echo "Targets:"
	@echo "  all          - Build binaries for all platforms (default)"
	@$(foreach plat,$(PLATFORMS),echo "  $(plat)  - Build for $(word 1,$(subst -, ,$(plat))) $(word 2,$(subst -, ,$(plat)))";)
	@echo "  clean        - Remove all built binaries"
	@echo "  help         - Show this help message"

.PHONY: all clean help
