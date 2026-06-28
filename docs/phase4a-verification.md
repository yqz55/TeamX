# Phase 4a — Admin CLI + HTTP 网关验证手册

> 验证目标：admin.exe 提供 CLI 子命令（list/show/history/kick/block/unblock）管理终端，
> 同时提供 HTTP 网关（ConnectRPC + WebSocket）让浏览器直接消费 gRPC RPC。

---

## 1. 架构概览

```
                               Admin CLI
                               ═════════
admin list     ──────────────── gRPC ListTerminals
admin show <id> ─────────────── gRPC GetTerminal          ┌─── TeamX Server (:50051)
admin history  ──────────────── gRPC GetTerminalHistory   │
admin kick     ──────────────── gRPC DisconnectTerminal ──┤    internal/server/
admin block    ──────────────── gRPC BlockTerminal        │    store/sqlite
admin unblock  ──────────────── gRPC UnblockTerminal      └───

                               Admin Gateway (:8080)
                               ═══════════════════════
Browser ── POST /teamx.proto.TeamX/ListTerminals ────────► Connect handler
                ...5 more unary RPCs...                         │
                                                                │ gRPC proxy
Browser ══ WS /ws  ◄── online/offline push ────────── wsHub ◄──┘ (poll ListTerminals every 5s)
                              CORS: Allow-Origin: *
```

### 新增/修改文件

| 文件 | 操作 | 说明 |
|---|---|---|
| `cmd/admin/main.go` | 重写 | Root cobra command + `--server` / `--json` 全局 flag + 7 子命令注册 |
| `cmd/admin/commands.go` | 新建 | 6 个 CLI 子命令（list/show/history/kick/block/unblock） |
| `cmd/admin/output.go` | 新建 | 格式化输出：表格（tabwriter）、详情块、JSON 模式 |
| `cmd/admin/serve.go` | 新建 | `admin serve` 子命令 — HTTP 网关启动 |
| `cmd/admin/gateway.go` | 新建 | ConnectRPC 代理 handler（6 RPC）+ wsHub 轮询广播 + CORS middleware |
| `buf.gen.yaml` | 修改 | 新增 `protoc-gen-connect-go` 插件 |
| `internal/proto/protoconnect/teamx.connect.go` | 新建 | buf 生成的 ConnectRPC 客户端/服务端接口（~350 行） |
| `CLAUDE.md` | 修改 | 架构图、生成命令、Phase 状态更新 |
| `go.mod` | 修改 | 新增 `spf13/cobra`、`connectrpc.com/connect`、`github.com/coder/websocket` |

---

## 2. CLI 子命令一览

| 命令 | 参数 | 对应 RPC | 输出 |
|---|---|---|---|
| `admin list` | `--status online/offline`, `--page`, `--page-size` | ListTerminals | 表格: CLIENT ID / HOSTNAME / OS / VERSION / STATUS / LAST HEARTBEAT |
| `admin show <id>` | `client_id`（必填） | GetTerminal | Summary 块 + Hardware 块 |
| `admin history <id>` | `--since`, `--until`, `--limit` | GetTerminalHistory | 时间线表格: REPORT ID / CREATED AT / CPU / MEMORY / DISKS / NETS |
| `admin kick <id>` | `client_id`（必填） | DisconnectTerminal | ✓ kicked / ✗ not found |
| `admin block <id>` | `client_id`（必填） | BlockTerminal | ✓ blocked / ✗ error |
| `admin unblock <id>` | `client_id`（必填） | UnblockTerminal | ✓ unblocked / ✗ error |

全局 flag：
- `--server` — gRPC 后端地址（默认 `localhost:50051`）
- `--json` — 输出 JSON 格式

---

## 3. HTTP 端点一览

| 方法 | 路径 | 请求体 | 说明 |
|---|---|---|---|
| POST | `/teamx.proto.TeamX/ListTerminals` | `{"page":1,"page_size":50,"online_filter":null}` | 终端列表 |
| POST | `/teamx.proto.TeamX/GetTerminal` | `{"client_id":"<id>"}` | 终端详情 + 硬件 |
| POST | `/teamx.proto.TeamX/GetTerminalHistory` | `{"client_id":"<id>","limit":100}` | 硬件历史 |
| POST | `/teamx.proto.TeamX/DisconnectTerminal` | `{"client_id":"<id>"}` | 踢断 |
| POST | `/teamx.proto.TeamX/BlockTerminal` | `{"client_id":"<id>"}` | 封禁 |
| POST | `/teamx.proto.TeamX/UnblockTerminal` | `{"client_id":"<id>"}` | 解封 |
| GET | `/ws` | —（WebSocket upgrade） | 实时上下线推送 |

`admin serve` flag：
- `--http-port` — HTTP 监听端口（默认 8080）
- `--server` — gRPC 后端地址（默认 `localhost:50051`）
- `--cors-origin` — CORS Allow-Origin（默认 `*`）
- `--poll-interval` — WebSocket 状态轮询间隔秒数（默认 5）

---

## 4. 编译

```bash
cd D:\MyProjects\TeamX
export GOPROXY=https://goproxy.cn,direct

# 全部二进制
go build -o bin/server.exe ./cmd/server/
go build -o bin/client.exe ./cmd/client/
go build -o bin/admin.exe    ./cmd/admin/

# 修改 proto 后重新生成
buf generate
```

---

## 5. CLI 验证

> 需要先启动 Server（终端 A），确保数据库有历史数据。

### 5.1 启动 Server

终端 A：

```powershell
cd D:\MyProjects\TeamX
.\bin\server.exe --port 50051 --db teamx.db
```

**预期输出**：

```
[store] schema migrated (16 tables)
database: teamx.db
TeamX Server listening on :50051
  heartbeat check interval: 10s, timeout: 30s
  max connections: 0 (0=unlimited)
```

### 5.2 admin list — 终端列表

终端 B：

```powershell
.\bin\admin.exe list
```

**预期输出**：

```
CLIENT ID                             HOSTNAME   OS     VERSION  STATUS   LAST HEARTBEAT
ff0e68d9-1e23-47c2-a962-3135096ddbf3  Ubuntu-24  linux  0.2.0    OFFLINE  2026-06-14T15:21:04Z
d3f86db0-defc-49ac-bf01-1aef35d6b4f6  Ubuntu-24  linux  0.2.0    ONLINE   2026-06-14T15:18:05Z
---
Total: 2 terminals (1 online, 1 offline)
```

### 5.3 admin list --status online

```powershell
.\bin\admin.exe list --status online
```

**预期输出**：只显示 `STATUS ONLINE` 的行。

### 5.4 admin list --json

```powershell
.\bin\admin.exe list --json
```

**预期输出**：

```json
{
  "terminals": [
    {
      "client_id": "ff0e68d9-1e23-47c2-a962-3135096ddbf3",
      "hostname": "Ubuntu-24",
      "os": "linux",
      "os_version": "Ubuntu 24.04.4 LTS",
      "client_version": "0.2.0",
      "last_heartbeat": "2026-06-14T15:21:04Z",
      "last_seen_at": "2026-06-14T15:18:14Z"
    },
    {
      "client_id": "d3f86db0-defc-49ac-bf01-1aef35d6b4f6",
      "hostname": "Ubuntu-24",
      "os": "linux",
      "os_version": "Ubuntu 24.04.4 LTS",
      "client_version": "0.2.0",
      "online": true,
      "last_heartbeat": "2026-06-14T15:18:05Z",
      "last_seen_at": "2026-06-14T15:13:55Z"
    }
  ],
  "total_count": 2
}
```

> ✅ JSON 格式输出正确，`online: true` 字段仅在在线时出现（proto3 default 省略）。

### 5.5 admin show — 终端详情

```powershell
.\bin\admin.exe show ff0e68d9-1e23-47c2-a962-3135096ddbf3
```

**预期输出**：

```
Summary:
  Client ID:    ff0e68d9-1e23-47c2-a962-3135096ddbf3
  Hostname:     Ubuntu-24
  OS:           linux (Ubuntu 24.04.4 LTS)
  Version:      0.2.0
  Status:       OFFLINE
  Last Seen:    2026-06-14T15:18:14Z
  First Seen:

Hardware:
  CPU:          AMD Ryzen 9 7945HX with Radeon Graphics (1 cores / 1 threads, amd64)
  Memory:       2.3 GB / 7.7 GB (30%)
  Disks:
    /dev/sda2    ext4   12.2 GB / 19.5 GB (62%)
  Network:
    lo           00:00:00:00:00:00  127.0.0.1, ::1
    ens33        00:0c:29:cf:0b:9e  192.168.235.132
  BIOS:         Phoenix Technologies LTD v6.00 (03/24/2025)
  Motherboard:  Intel Corporation 440BX Desktop Reference Platform / SN: -
```

> ✅ 展示终端摘要 + 完整硬件信息（CPU/内存/磁盘/网络/BIOS/主板）。

### 5.6 admin history — 硬件历史

```powershell
.\bin\admin.exe history d3f86db0-defc-49ac-bf01-1aef35d6b4f6 --limit 3
```

**预期输出**：

```
REPORT ID                             CREATED AT            CPU                        MEMORY         DISKS  NETS
ec14c9b4-46db-4130-b7b8-f092766f3995  2026-06-14T15:17:55Z  AMD Ryzen 9 7945HX wit...  2.3 GB/7.7 GB  1      2
1b42d196-6d0e-45ad-8c11-f9b1749b42ac  2026-06-14T15:17:25Z  AMD Ryzen 9 7945HX wit...  2.3 GB/7.7 GB  1      2
084480e3-f430-46e8-b378-78e6b5154e9a  2026-06-14T15:16:55Z  AMD Ryzen 9 7945HX wit...  2.3 GB/7.7 GB  1      2
---
3 snapshots for d3f86db0-defc-49ac-bf01-1aef35d6b4f6
```

> ✅ 硬件快照按时间倒序排列，支持 `--since` / `--until` 时间范围过滤。

### 5.7 admin kick — 踢断终端

```powershell
.\bin\admin.exe kick d3f86db0-defc-49ac-bf01-1aef35d6b4f6
```

**预期输出**（终端在线时）：`✓ kicked`
**预期输出**（终端离线或不存在时）：`✗ terminal not found or offline`

### 5.8 admin block — 封禁终端

```powershell
.\bin\admin.exe block ff0e68d9-1e23-47c2-a962-3135096ddbf3
```

**预期输出**：`✓ blocked`

验证封禁生效（同 hostname 无法注册）：

```powershell
# 对应 hostname 的 Client 尝试重连 → Server 日志：
# [register] rejected: hostname=Ubuntu-24 is blocked
```

### 5.9 admin unblock — 解封终端

```powershell
.\bin\admin.exe unblock ff0e68d9-1e23-47c2-a962-3135096ddbf3
```

**预期输出**：`✓ unblocked`

### 5.10 错误处理

```powershell
.\bin\admin.exe list --server localhost:9999
```

**预期输出**：

```
Error: rpc error: code = Unavailable desc = ... connection refused
```

退出码非零（`echo %ERRORLEVEL%` → `1`）。

> ✅ 连接失败时错误只输出一次，退出码正确。

---

## 6. HTTP 网关验证

> 需要先启动 Server（终端 A），再启动 Gateway（终端 B）。

### 6.1 启动 Gateway

终端 A（Server 已跑）：

```powershell
cd D:\MyProjects\TeamX
.\bin\admin.exe serve --http-port 8080 --poll-interval 5
```

**预期输出**：

```
TeamX Admin Gateway listening on :8080
  gRPC backend: localhost:50051
  CORS origin:  *
  WS poll:      every 5s
```

### 6.2 ListTerminals

```bash
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/ListTerminals \
  -H "Content-Type: application/json" \
  -d '{"page":1,"page_size":50}'
```

**预期输出**：JSON 数组，包含 `terminals` 和 `totalCount` 字段。

### 6.3 GetTerminal

```bash
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/GetTerminal \
  -H "Content-Type: application/json" \
  -d '{"client_id":"<client-id>"}'
```

**预期输出**：JSON 含 `summary` + `latestHardware` 对象。

### 6.4 GetTerminalHistory

```bash
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/GetTerminalHistory \
  -H "Content-Type: application/json" \
  -d '{"client_id":"<client-id>","limit":2}'
```

**预期输出**：JSON 含 `snapshots` 数组。

### 6.5 DisconnectTerminal

```bash
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/DisconnectTerminal \
  -H "Content-Type: application/json" \
  -d '{"client_id":"<client-id>"}'
```

**预期输出**：`{"ok":true,"message":"kicked"}` 或 `{"message":"terminal not found or offline"}`。

### 6.6 BlockTerminal / UnblockTerminal

```bash
# Block
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/BlockTerminal \
  -H "Content-Type: application/json" \
  -d '{"client_id":"<client-id>"}'
# → {"ok":true,"message":"blocked"}

# Unblock
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/UnblockTerminal \
  -H "Content-Type: application/json" \
  -d '{"client_id":"<client-id>"}'
# → {"ok":true,"message":"unblocked"}
```

### 6.7 CORS 预检

```bash
curl -s -I -X OPTIONS http://localhost:8080/teamx.proto.TeamX/ListTerminals \
  -H "Origin: http://localhost:5173" \
  -H "Access-Control-Request-Method: POST"
```

**预期响应头**：

```
HTTP/1.1 204 No Content
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: POST, GET, OPTIONS
Access-Control-Allow-Headers: Content-Type, Connect-Protocol-Version, X-User-Agent
```

> ✅ CORS preflight 返回 204，前端 `localhost:5173` 可跨域调用。

### 6.8 WebSocket

使用浏览器控制台或 `wscat` 工具连接：

```bash
# 需要安装: npm i -g wscat 或用浏览器直接测试
wscat -c ws://localhost:8080/ws
```

**预期**：连接保持打开，当终端上下线时收到 JSON 推送：

```json
{"type":"online", "client_id":"xxx", "hostname":"vm-linux",   "timestamp":"2026-06-16T..."}
{"type":"offline","client_id":"yyy", "hostname":"win-dev",    "timestamp":"2026-06-16T..."}
```

> Gateway 每 5 秒轮询 Server 的 `ListTerminals`，对比上次快照，检测状态变化后广播。

### 6.9 浏览器最小验证

在浏览器控制台中直接测试（Gateway 启动后）：

```js
// 列表查询
fetch('http://localhost:8080/teamx.proto.TeamX/ListTerminals', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ page: 1, page_size: 10 })
}).then(r => r.json()).then(console.log)

// WebSocket
const ws = new WebSocket('ws://localhost:8080/ws')
ws.onmessage = e => console.log('WS:', JSON.parse(e.data))
```

**预期**：`fetch` 返回 JSON 终端列表，`ws` 连接成功，5 秒后可能收到状态变化事件。

---

## 7. 通过标准

| # | 验证项 | 预期 | 状态 |
|---|--------|------|------|
| 1 | `go build ./...` | 三个二进制编译通过 | ☐ |
| 2 | `buf generate` | Connect 代码正常生成 | ☐ |
| 3 | `admin list` | 表格输出终端列表，含 total 汇总 | ☐ |
| 4 | `admin list --status online` | 只显示在线终端 | ☐ |
| 5 | `admin list --json` | JSON 数组 + total_count | ☐ |
| 6 | `admin show <id>` | Summary + 完整 Hardware 块 | ☐ |
| 7 | `admin history <id>` | 硬件快照时间线 | ☐ |
| 8 | `admin history --since/--until` | 时间范围过滤生效 | ☐ |
| 9 | `admin kick <id>` | 在线终端被踢断，离线终端提示 "not found or offline" | ☐ |
| 10 | `admin block <id>` | 返回 ✓ blocked | ☐ |
| 11 | `admin unblock <id>` | 返回 ✓ unblocked | ☐ |
| 12 | 连接失败错误处理 | 错误只输出一次，退出码非零 | ☐ |
| 13 | HTTP ListTerminals | curl POST → JSON 正确 | ☐ |
| 14 | HTTP GetTerminal | curl POST → summary + latestHardware | ☐ |
| 15 | HTTP GetTerminalHistory | curl POST → snapshots[] | ☐ |
| 16 | HTTP DisconnectTerminal | curl POST → ok/message | ☐ |
| 17 | HTTP BlockTerminal | curl POST → blocked | ☐ |
| 18 | HTTP UnblockTerminal | curl POST → unblocked | ☐ |
| 19 | CORS preflight (OPTIONS) | 204 + Allow-Origin / Allow-Methods / Allow-Headers | ☐ |
| 20 | WebSocket /ws 连接 | 连接成功，上下线时收到 JSON 推送 | ☐ |
| 21 | CLI 与 Gateway 共存 | `admin list` 和 `admin serve` 在同一二进制中正常 | ☐ |

全部勾选 = **Phase 4a 完成** 🎉

---

## 8. 已知限制

| 限制 | 说明 | 计划 |
|---|---|---|
| WebSocket 轮询 | 每 5 秒全量查询状态变化，非实时推送 | Phase 5 可由 Server 主动推送事件 |
| 无认证 | HTTP 网关和 CLI 无需鉴权 | Phase 11 加 JWT/RBAC |
| 无 HTTPS | Gateway 仅 HTTP，无 TLS | Phase 11 加 TLS |
| CORS 默认 `*` | 生产环境不安全 | 生产改为具体 origin |
| Connect 流式未暴露 | Register / Channel / TransferFile 不通过 HTTP 暴露 | 仅限 gRPC |
