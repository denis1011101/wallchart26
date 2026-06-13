GO ?= /usr/local/go/bin/go
GOCACHE ?= /tmp/wallchart26-go-build
TEMPL ?= templ

.PHONY: build generate run test

generate:
	$(TEMPL) generate

build:
	GOCACHE=$(GOCACHE) CGO_ENABLED=0 $(GO) build -buildvcs=false -o wallchart26 .

run: build
	COOKIE_SECURE=false ./wallchart26

test:
	GOCACHE=$(GOCACHE) $(GO) test ./...
