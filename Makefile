.PHONY: help build run test clean lint fmt vet tidy docker-build docker-run

BINARY_NAME=majordomo-proxy
BUILD_DIR=bin

help:
	@echo "Available targets:"
	@echo "  build          - Build the binary"
	@echo "  run            - Build and run the server"
	@echo "  test           - Run all tests"
	@echo "  test-cover     - Run tests with coverage report"
	@echo "  clean          - Remove build artifacts"
	@echo "  lint           - Run golangci-lint"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  tidy           - Run go mod tidy"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-run     - Run Docker container"

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/majordomo

run: build
	./$(BUILD_DIR)/$(BINARY_NAME) serve

test:
	go test -v ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

lint:
	golangci-lint run

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

docker-build:
	docker build -t $(BINARY_NAME) .

docker-run:
	docker run -p 8080:8080 $(BINARY_NAME)
