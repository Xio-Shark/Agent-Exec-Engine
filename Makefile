.PHONY: build run test clean deps lint demo-workflow

BINARY := agent-exec-engine
CMD    := ./cmd/server

build:
	go build -o bin/$(BINARY) $(CMD)

run:
	go run $(CMD)

demo-workflow:
	bash ./scripts/run_demo_workflow.sh

test:
	go test ./... -v -race -count=1

test-cover:
	go test ./... -coverprofile=coverage.out -race
	go tool cover -html=coverage.out -o coverage.html

deps:
	go mod tidy
	go mod download

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ coverage.out coverage.html

docker-build:
	docker build -t $(BINARY):latest -f deployments/Dockerfile .

docker-compose-up:
	docker-compose -f deployments/docker-compose.yaml up -d

docker-compose-down:
	docker-compose -f deployments/docker-compose.yaml down
