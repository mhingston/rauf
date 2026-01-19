BINARY_NAME ?= rauf
VERSION ?= 1.3.3
BUILD_DIR ?= dist

GOOS_LIST ?= darwin linux windows
GOARCH_LIST ?= amd64 arm64

.PHONY: build test lint fmt clean dist

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY_NAME) ./cmd/rauf

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w ./cmd/rauf/*.go

clean:
	rm -rf $(BUILD_DIR)

# Build cross-platform binaries into dist/
dist:
	@mkdir -p $(BUILD_DIR)
	@for os in $(GOOS_LIST); do \
		for arch in $(GOARCH_LIST); do \
			out=$(BUILD_DIR)/$(BINARY_NAME)-$$os-$$arch; \
			if [ "$$os" = "windows" ]; then out=$$out.exe; fi; \
			echo "Building $$out"; \
			GOOS=$$os GOARCH=$$arch go build -ldflags "-X main.version=$(VERSION)" -o $$out ./cmd/rauf; \
		done; \
	done
