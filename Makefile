# bv Makefile
#
# Build with SQLite FTS5 (full-text search) support enabled

.PHONY: build install clean test

# Enable FTS5 for full-text search in SQLite exports
export CGO_CFLAGS := -DSQLITE_ENABLE_FTS5

build:
	go build -o bv ./cmd/bv
	go build -o wbd ./cmd/wbd
	go build -o wbv ./cmd/wbv

install:
	go install ./cmd/bv ./cmd/wbd ./cmd/wbv

clean:
	rm -f bv wbd wbv
	go clean

test:
	go test ./...
