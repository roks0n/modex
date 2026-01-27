APP := modex
DIST_DIR := dist

.PHONY: help build install clean dist

help:
	@printf "Targets:\n"
	@printf "  build    Build local binary\n"
	@printf "  install  Install to Go bin\n"
	@printf "  dist     Build Linux/macOS binaries\n"
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
