all: build

## build
build:
	GOOS=linux GOARCH=amd64 go build -ldflags '-s -w' -o dist/mockserver_linux main.go $<
	GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o dist/mockserver_windows main.go $<

.PHONY: build