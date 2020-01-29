# meta
MAIN := main.go
LDFLAGS := -X 'main.version=$(VERSION)' -X 'main.revision=$(REVISION)'
GOPATH=$(shell go env GOPATH)

## env
export GO111MODULE=on
export GOOS=linux
export GOARCH=amd64

all: build

## build
build:
	GOOS=linux GOARCH=amd64 go build -ldflags '-s -w' -o dist/mockserver_linux main.go $<
	GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o dist/mockserver_windows main.go $<

.PHONY: build