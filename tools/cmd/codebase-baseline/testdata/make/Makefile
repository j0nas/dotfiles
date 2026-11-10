.PHONY: test lint build deploy
VERSION := 1

test:
	docker compose up -d db
	go test ./...

lint: deps
	golangci-lint run

build:
	go build ./...

clean:
	rm -rf bin
