# Run the app locally
dev:
	go run .

# Run tests
test:
	go test ./...

# Build the app
build:
	go build

# Format & Lint
lint:
	golangci-lint fmt && golangci-lint run --fix
