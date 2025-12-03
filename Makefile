# Makefile for cross-compiling Go binaries for multiple platforms and architectures

# Output directory for binaries
BIN_DIR = bin

# Go build flags
GOFLAGS = -ldflags="-s -w" -trimpath

# Define OS-ARCH pairs
PLATFORMS = \
	darwin-amd64 \
	darwin-arm64 \
	linux-amd64 \
	linux-arm64

# Auto-discover commands in cmd directory
CMDS = $(notdir $(wildcard cmd/*))

# Generate target list for all commands and platforms
TARGETS = $(foreach cmd,$(CMDS),$(foreach plat,$(PLATFORMS),$(BIN_DIR)/livekit-$(cmd)-$(plat)))

# Default target: build for all commands and platforms
all: $(TARGETS)

# Define a template for build rules per command and platform
define BUILD_RULE
$(BIN_DIR)/livekit-$(1)-$(2):
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(word 1,$(subst -, ,$(2))) GOARCH=$(word 2,$(subst -, ,$(2))) go build $(GOFLAGS) -o $$@ ./cmd/$(1)

.PHONY: $(1)-$(2)
$(1)-$(2): $(BIN_DIR)/livekit-$(1)-$(2)
endef

# Apply the build rule template to each command and platform combination
$(foreach cmd,$(CMDS),$(foreach plat,$(PLATFORMS),$(eval $(call BUILD_RULE,$(cmd),$(plat)))))

# Removed per-command targets to build all platforms for a specific command

# Clean up binaries
clean:
	rm -rf $(BIN_DIR)

# Help message
help:
	@echo "Makefile for cross-compiling LiveKit binaries"
	@echo "Targets:"
	@echo "  all          - Build binaries for all commands and platforms (default)"
	@echo "  <cmd>-<platform> - Build specific command for a platform (cmds: $(CMDS); platforms: $(PLATFORMS))"
	@echo "  clean        - Remove all built binaries"
	@echo "  help         - Show this help message"

.PHONY: all clean help
