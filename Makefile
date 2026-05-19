BINARY  = tmux-process-monitor
CMD     = ./cmd/tmux-process-monitor
GOFLAGS = -ldflags="-s -w"

.PHONY: build build-all install test clean

build:
	go build $(GOFLAGS) -o bin/$(BINARY) $(CMD)

build-all:
	mkdir -p dist
	GOOS=linux  GOARCH=amd64  go build $(GOFLAGS) -o dist/$(BINARY)-linux-amd64  $(CMD)
	GOOS=linux  GOARCH=arm64  go build $(GOFLAGS) -o dist/$(BINARY)-linux-arm64  $(CMD)
	GOOS=darwin GOARCH=amd64  go build $(GOFLAGS) -o dist/$(BINARY)-darwin-amd64 $(CMD)
	GOOS=darwin GOARCH=arm64  go build $(GOFLAGS) -o dist/$(BINARY)-darwin-arm64 $(CMD)

install: build

test:
	go test ./...

clean:
	rm -rf bin/ dist/
