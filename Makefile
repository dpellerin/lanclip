VERSION ?= 0.1.0-dev
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: all build test fmt vet clean

all: test build

build:
	mkdir -p bin
	go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o bin/lanclip ./cmd/lanclip

test:
	go test -buildvcs=false ./...

fmt:
	gofmt -w $$(find cmd internal test -name '*.go' -type f)

vet:
	go vet -buildvcs=false ./...

clean:
	rm -rf bin dist
