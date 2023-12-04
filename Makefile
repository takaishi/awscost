default: build

.PHONY: test
test:
	go test -race ./...

.PHONY: build
build:
	go build -o dist/main .
