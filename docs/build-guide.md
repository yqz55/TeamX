# TeamX 编译说明

## 目录

- [1. 环境要求](#1-环境要求)
- [2. Go 后端编译](#2-go-后端编译)
- [3. 前端编译](#3-前端编译)
- [4. Proto 代码生成](#4-proto-代码生成)
- [5. Linux 交叉编译](#5-linux-交叉编译)
- [6. 一键编译脚本](#6-一键编译脚本)

---

## 1. 环境要求

| 工具 | 版本 | 说明 |
|---|---|---|
| Go | 1.26 | 后端语言 |
| Node.js | ≥ 18 | 前端构建（当前 v24.13.1） |
| npm | ≥ 9 | 前端包管理 |
| buf | ≥ 1.50 | Proto 代码生成 |
| Git | 任意 | 版本管理 |

### 网络配置

由于国内网络环境，编译前需要设置镜像：

```powershell
# Go 代理
set GOPROXY=https://goproxy.cn,direct

# npm 镜像
npm config set registry https://registry.npmmirror.com
```

---

## 2. Go 后端编译

### 2.1 三个二进制

```powershell
cd D:\MyProjects\TeamX
set GOPROXY=https://goproxy.cn,direct

# 服务端
go build -o bin/server.exe ./cmd/server/

# 客户端（被管控端）
go build -o bin/client.exe ./cmd/client/

# 管理 CLI（含 HTTP 网关）
go build -o bin/admin.exe    ./cmd/admin/
```

产物均在 `bin/`（gitignored）：

```
bin/
├── server.exe    # gRPC Server (:50051)
├── client.exe    # Agent (Windows)
└── admin.exe     # CLI + HTTP Gateway (:8080)
```

### 2.2 工具脚本编译

`tools/` 目录下各脚本含 `func main()`，需单独编译：

```powershell
# 编译并运行（不生成独立 exe）
go run tools/dump_db.go
go run tools/check_rpc.go
go run tools/check_33.go

# 或编译到 bin/
go build -o bin/dump_db.exe tools/dump_db.go
go build -o bin/check_rpc.exe tools/check_rpc.go
```

> `go build ./tools/...` 会因多个 `main` 函数报错，这是预期行为。每个文件需单独指定。

---

## 3. 前端编译

### 3.1 安装依赖

```powershell
cd D:\MyProjects\TeamX\web
npm install
```

安装约 150 个包，取决于当前版本。

### 3.2 生成 Proto 代码

```powershell
npm run generate
```

产出：
- `web/src/gen/teamx_pb.ts` — 消息类型（约 80KB）
- `web/src/gen/teamx_connect.ts` — Connect 服务客户端（约 3KB）

> 修改 `internal/proto/teamx.proto` 后需要运行此命令重新生成。

### 3.3 开发模式

```powershell
npm run dev
```

Vite dev server 启动在 `http://localhost:5173`，支持热更新。

### 3.4 生产构建

```powershell
npm run build
```

等价于 `tsc -b && vite build`，产物在 `web/dist/`。

### 3.5 版本兼容

| 包 | 版本 | 说明 |
|---|---|---|
| `antd` | ^5.22 | 组件库 + 图标 |
| `react` / `react-dom` | ^19.1 | 前端框架 |
| `@connectrpc/connect` | ^1.7 | Connect RPC 客户端 |
| `@connectrpc/connect-web` | ^1.7 | HTTP 传输层 |
| `@bufbuild/protobuf` | ^1.10 | Proto 运行时 |
| `@bufbuild/protoc-gen-es` | ^1.10 | Proto TS 代码生成插件 |
| `@connectrpc/protoc-gen-connect-es` | ^1.7 | Connect TS 代码生成插件 |

> 当前使用 v1 系列，因 `protoc-gen-connect-es` v2 尚未发布。升级时只需改版本号 + 重新 `npm run generate`。

---

## 4. Proto 代码生成

### 4.1 生成流程

修改 `internal/proto/teamx.proto` 后，需分别生成 Go 和 TypeScript 代码：

```powershell
# 1. Go 代码（从项目根目录执行）
cd D:\MyProjects\TeamX
buf generate

# 2. TypeScript 代码（从 web/ 执行）
cd web
npm run generate
```

### 4.2 生成产物总览

| 文件 | 生成工具 | 说明 |
|---|---|---|
| `internal/proto/teamx.pb.go` | protoc-gen-go | Go 消息类型 |
| `internal/proto/teamx_grpc.pb.go` | protoc-gen-go-grpc | Go gRPC 桩 |
| `internal/proto/protoconnect/teamx.connect.go` | protoc-gen-connect-go | Go Connect 处理器 |
| `web/src/gen/teamx_pb.ts` | protoc-gen-es | TS 消息类型 |
| `web/src/gen/teamx_connect.ts` | protoc-gen-connect-es | TS Connect 客户端 |

### 4.3 配置文件

| 文件 | 说明 |
|---|---|
| `buf.gen.yaml` | Go 代码生成（根目录，3 个 Go 插件） |
| `web/buf.gen.yaml` | TS 代码生成（web 目录，2 个 JS 插件） |
| `buf.yaml` | Buf 模块定义（proto 源目录） |

### 4.4 新增 Proto 字段后的检查

添加新字段后确保：
1. `buf generate` + `cd web && npm run generate` 都执行
2. Go 编译通过：`go build ./cmd/server/ ./cmd/admin/ ./cmd/client/`
3. TS 编译通过：`cd web && npx tsc -b`
4. Server 端填充新字段（`internal/server/server.go`）

---

## 5. Linux 交叉编译

Linux VM 测试环境 `192.168.235.132`，用户 `yqz`，公钥认证。

### 5.1 编译 + 部署

```powershell
# 1. 交叉编译
cd D:\MyProjects\TeamX
GOOS=linux GOARCH=amd64 go build -o bin/client-linux ./cmd/client/

# 2. 推送到 VM
scp -o StrictHostKeyChecking=no bin/client-linux yqz@192.168.235.132:/home/yqz/client-linux

# 3. 启动（在 VM 上，<host-ip> 为宿主机 VMnet 网卡 IP）
ssh -o StrictHostKeyChecking=no yqz@192.168.235.132 "/home/yqz/client-linux --server <host-ip>:50051"
```

### 5.2 查找宿主机 IP

```powershell
# 宿主机上查看 VMnet 网卡 IP
ipconfig | findstr "192.168"

# 或从 VM 查看网关
ssh yqz@192.168.235.132 "ip route | grep default"
```

---

## 6. 一键编译脚本

### 6.1 全部编译（Windows PowerShell）

```powershell
# full-build.ps1
$ErrorActionPreference = "Stop"
$env:GOPROXY = "https://goproxy.cn,direct"

Write-Host "=== 1. Backend ==="
go build -o bin/server.exe ./cmd/server/
go build -o bin/client.exe ./cmd/client/
go build -o bin/admin.exe   ./cmd/admin/
Write-Host "server.exe / client.exe / admin.exe 编译完成"

Write-Host "=== 2. Linux cross-compile ==="
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -o bin/client-linux ./cmd/client/
$env:GOOS=""; $env:GOARCH=""
Write-Host "client-linux 编译完成"

Write-Host "=== 3. Frontend ==="
Push-Location web
npm run generate
npm run build
Pop-Location
Write-Host "dist/ 构建完成"

Write-Host "=== 编译完成 ==="
```

### 6.2 仅后端（跳过前端）

```powershell
set GOPROXY=https://goproxy.cn,direct
go build -o bin/server.exe ./cmd/server/
go build -o bin/client.exe ./cmd/client/
go build -o bin/admin.exe   ./cmd/admin/
```

### 6.3 仅前端（跳过后端）

```powershell
cd web
npm run generate
npm run build
```
