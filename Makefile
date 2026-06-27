.PHONY: build build-server test lint run-repl run-mcp clean

BINARY := bin/leash
CMD := ./cmd/leash
SERVER_BINARY := bin/server
SERVER_CMD := ./cmd/server
POLICY ?= policies/default.yaml
MCP_ADDR ?= :8080

build:
	go build -o $(BINARY) $(CMD)

build-server:
	go build -o $(SERVER_BINARY) $(SERVER_CMD)

test:
	go test ./...

lint:
	go vet ./...
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed, skipping"

run-repl: build
	./$(BINARY) --policy $(POLICY) repl

run-mcp: build
	./$(BINARY) --policy $(POLICY) mcp stdio

clean:
	rm -f $(BINARY) $(SERVER_BINARY)
