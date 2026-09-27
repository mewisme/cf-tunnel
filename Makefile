BINARY := cf-tunnel
CMD := ./cmd/$(BINARY)
BIN_DIR := bin

.PHONY: all fmt fmt-check vet test build check clean release-check release-snapshot

all: check build

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './dist/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './dist/*'))" || \
		(echo "Go files need formatting"; gofmt -l $$(find . -name '*.go' -not -path './dist/*'); exit 1)

vet:
	go vet ./...

test:
	go test ./...

build:
	mkdir -p $(BIN_DIR)
	go build -trimpath -o $(BIN_DIR)/$(BINARY) $(CMD)

check: fmt-check vet test

release-check:
	goreleaser check

release-snapshot:
	@command -v upx >/dev/null || (echo "UPX is required for release snapshots"; exit 1)
	goreleaser release --snapshot --clean

clean:
	rm -rf $(BIN_DIR) dist