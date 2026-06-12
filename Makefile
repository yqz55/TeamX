.PHONY: all build build-client build-server build-admin proto test clean run-server run-client

# Go 编译参数
GO           := go
GOFLAGS      := -trimpath -ldflags="-s -w"
OUT_DIR      := build

# 默认目标
all: build

# 编译所有
build: build-server build-client build-admin

build-server:
	@mkdir -p $(OUT_DIR)
	$(GO) build $(GOFLAGS) -o $(OUT_DIR)/teamx-server ./cmd/server

build-client:
	@mkdir -p $(OUT_DIR)
	$(GO) build $(GOFLAGS) -o $(OUT_DIR)/teamx-client ./cmd/client

build-admin:
	@mkdir -p $(OUT_DIR)
	$(GO) build $(GOFLAGS) -o $(OUT_DIR)/teamx-admin ./cmd/admin

# 生成 protobuf 代码
proto:
	protoc --go_out=. --go-grpc_out=. internal/proto/*.proto

# 运行
run-server: build-server
	./$(OUT_DIR)/teamx-server

run-client: build-client
	./$(OUT_DIR)/teamx-client

# 测试
test:
	$(GO) test -v -race -count=1 ./...

# 代码检查
lint:
	golangci-lint run ./...

# 清理
clean:
	rm -rf $(OUT_DIR)

# 跨平台编译（Phase 10 用）
cross-build:
	GOOS=linux   GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(OUT_DIR)/teamx-client-linux-amd64   ./cmd/client
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(OUT_DIR)/teamx-client-windows-amd64.exe ./cmd/client
	GOOS=darwin  GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(OUT_DIR)/teamx-client-darwin-amd64  ./cmd/client
