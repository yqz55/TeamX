.PHONY: build build-server build-client build-admin proto test lint clean

# Build all binaries
build: build-server build-client build-admin

build-server:
	cd cmd/server && go build -o ../../bin/server .

build-client:
	cd cmd/client && go build -o ../../bin/client .

build-admin:
	cd cmd/admin && go build -o ../../bin/admin .

# Cross-platform builds
build-all:
	GOOS=linux GOARCH=amd64 go build -o bin/server-linux-amd64 ./cmd/server/
	GOOS=linux GOARCH=amd64 go build -o bin/client-linux-amd64 ./cmd/client/
	GOOS=windows GOARCH=amd64 go build -o bin/server-windows-amd64.exe ./cmd/server/
	GOOS=windows GOARCH=amd64 go build -o bin/client-windows-amd64.exe ./cmd/client/
	GOOS=darwin GOARCH=amd64 go build -o bin/server-darwin-amd64 ./cmd/server/
	GOOS=darwin GOARCH=amd64 go build -o bin/client-darwin-amd64 ./cmd/client/

# Generate protobuf code
proto:
	protoc --go_out=. --go-grpc_out=. internal/proto/*.proto

# Run tests
test:
	go test -v -race ./...

# Run linter
lint:
	golangci-lint run ./...

# Clean build artifacts
clean:
	rm -rf bin/
