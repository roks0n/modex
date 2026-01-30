APP := modex
DIST_DIR := dist

.PHONY: help build install clean dist release

help:
	@printf "Targets:\n"
	@printf "  build    Build local binary\n"
	@printf "  install  Install to Go bin\n"
	@printf "  dist     Build Linux/macOS binaries\n"
	@printf "  release  Create and push version tag (VERSION=v1.0.0)\n"
	@printf "  clean    Remove build artifacts\n"

build:
	go build -o $(APP)

install:
	go install ./...

dist:
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(DIST_DIR)/$(APP)-linux-amd64
	GOOS=linux GOARCH=arm64 go build -o $(DIST_DIR)/$(APP)-linux-arm64
	GOOS=darwin GOARCH=amd64 go build -o $(DIST_DIR)/$(APP)-darwin-amd64
	GOOS=darwin GOARCH=arm64 go build -o $(DIST_DIR)/$(APP)-darwin-arm64

clean:
	rm -rf $(APP) $(DIST_DIR)

# Release version guide (Semantic Versioning):
#   MAJOR (X.0.0) - Breaking changes that require user action
#   MINOR (x.Y.0) - New features, backwards compatible
#   PATCH (x.y.Z) - Bug fixes, backwards compatible
#
# Examples:
#   v1.0.0 -> v1.0.1  Bug fix, no breaking changes
#   v1.0.0 -> v1.1.0  New feature added, still compatible
#   v1.0.0 -> v2.0.0  Breaking change, users must update their usage

release:
ifndef VERSION
	@echo "Error: VERSION is required. Usage: make release VERSION=v1.0.0"
	@echo ""
	@echo "Versioning guide:"
	@echo "  MAJOR (X.0.0) - Breaking changes (e.g., v1.0.0 -> v2.0.0)"
	@echo "  MINOR (x.Y.0) - New features (e.g., v1.0.0 -> v1.1.0)"
	@echo "  PATCH (x.y.Z) - Bug fixes (e.g., v1.0.0 -> v1.0.1)"
	@exit 1
endif
	@echo "Creating release $(VERSION)..."
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	@echo "Release $(VERSION) pushed! GitHub Actions will build and publish binaries."
