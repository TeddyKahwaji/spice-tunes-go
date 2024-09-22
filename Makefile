.DEFAULT_GOAL := build
BIN_FILE := app

build: 
	go build cmd/main.go 

run:
	docker compose up --remove-orphans

clean:
	docker compose down 

lint:
	golangci-lint run --enable-all

gofumpt: 
	gofumpt -l -w .