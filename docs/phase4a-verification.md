# Phase 4a — Admin CLI + HTTP 网关验证手册

> 验证目标：admin.exe 提供 CLI 子命令管理终端（基于 device_id / session_id 双标识），
> 同时提供 HTTP 网关（ConnectRPC + WebSocket）让浏览器直接消费 gRPC RPC。
>
> **协议更新**：4a 开发过程中引入了设备指纹（device_id）与会话标识（session_id）分离，
> 封禁粒度从 hostname 改为 device_id，kick 返回 PermissionDenied 阻止客户端自动重连。

---

## 1. 架构概览

```
                               Admin CLI
                               ═════════
admin list     ──────────────── gRPC ListTerminals
admin show <id> ─────────────── gRPC GetTerminal          ┌─── TeamX Server (:50051)
admin history <did> ─────────── gRPC GetTerminalHistory   │
admin kick <sid> ────────────── gRPC DisconnectTerminal ──┤    internal/server/
admin block <did> ───────────── gRPC BlockTerminal        │    store/sqlite
admin unblock <did> ─────────── gRPC UnblockTerminal      └───

                               Admin Gateway (:8080)
                               ═══════════════════════
Browser ── POST /teamx.proto.TeamX/ListTerminals ────────► Connect handler
                ...5 more unary RPCs...                         │
                                                                │ gRPC proxy
Browser ══ WS /ws  ◄── online/offline push ────────── wsHub ◄──┘ (poll ListTerminals every 5s)
                              CORS: Allow-Origin: *
```

### 1.1 核心概念：device_id vs session_id

| 概念 | 来源 | 稳定性 | 用途 |
|---|---|---|---|
| **device_id** | 硬件指纹 SHA-256（DMI UUID + MAC + 磁盘序列号 + machine-id） | 永久不变（同物理机） | 封禁、历史查询 |
| **session_id** | Server 分配的 UUID v4 | 每次连接不同 | 踢断当前会话、心跳追踪 |

```
Client (device_id = abc123...)
  │
  ├── 第 1 次连接 ─► session_id = s1-xxxx  (Register 分配)
  ├── 第 2 次连接 ─► session_id = s2-yyyy  (同上设备，不同会话)
  └── 第 3 次连接 ─► session_id = s3-zzzz

admin block abc123...  → 封禁该设备所有会话 + 阻止新注册
admin kick  s2-yyyy    → 仅踢断该次会话，设备可立即重连
```

### 1.2 新增/修改文件

| 文件 | 操作 | 说明 |
|---|---|---|
| `cmd/admin/main.go` | 重写 | Root cobra command + `--server` / `--json` 全局 flag + 7 子命令 |
| `cmd/admin/commands.go` | 新建 | 6 个 CLI 子命令（参数使用 session_id / device_id） |
| `cmd/admin/output.go` | 新建 | 格式化输出（tabwriter 表格含 SESSION ID + DEVICE ID 双列） |
| `cmd/admin/serve.go` | 新建 | `admin serve` 子命令 |
| `cmd/admin/gateway.go` | 新建 | ConnectRPC 代理 + wsHub 轮询广播 + CORS |
| `internal/proto/teamx.proto` | 修改 | HandshakeRequest 加 `device_id`；`client_id` → `session_id`；Block 按 device_id |
| `internal/proto/protoconnect/teamx.connect.go` | 新建 | buf 生成 ConnectRPC 代码 |
| `internal/client/fingerprint.go` | 新建 | `GenerateDeviceID()` — 硬件指纹 + 本地缓存 |
| `internal/client/fingerprint_linux.go` | 新建 | Linux: DMI UUID + machine-id + MAC + 磁盘序列号 |
| `internal/client/fingerprint_windows.go` | 新建 | Windows 骨架（Phase 10 完善） |
| `internal/server/server.go` | 修改 | Register 用 device_id；kick 返回 PermissionDenied；Block 踢断所有会话 |
| `internal/server/connection.go` | 修改 | ClientConn 加 DeviceID；map 键改为 sessionID；metadata header 改为 session-id |
| `internal/server/store/*.go` | 修改 | terminals 表 PK 改为 session_id，新增 device_id 列；Block 按 device_id |
| `internal/client/client.go` | 修改 | Register 携带 device_id + 检查 Ok 标志 + fatalError 处理 |
| `buf.gen.yaml` | 修改 | 新增 Connect 插件 |
| `CLAUDE.md` | 修改 | 架构图、命令、Phase 状态 |

---

## 2. CLI 子命令一览

| 命令 | 参数 | 对应 RPC | 输出 |
|---|---|---|---|
| `admin list` | `--status online/offline`, `--page`, `--page-size` | ListTerminals | 表格: SESSION ID / DEVICE ID / HOSTNAME / OS / VERSION / STATUS / LAST HEARTBEAT |
| `admin show <id>` | session_id 或 device_id（自动识别：64 字符 = device_id） | GetTerminal | Summary 块（含 Session ID + Device ID）+ Hardware 块 |
| `admin history <device-id>` | `--since`, `--until`, `--limit` | GetTerminalHistory | 时间线表格: REPORT ID / CREATED AT / CPU / MEMORY / DISKS / NETS |
| `admin kick <session-id>` | session_id（必填） | DisconnectTerminal | ✓ kicked / ✗ session not found or offline |
| `admin block <device-id>` | device_id（必填） | BlockTerminal | ✓ blocked / ✗ error |
| `admin unblock <device-id>` | device_id（必填） | UnblockTerminal | ✓ unblocked / ✗ error |

全局 flag：
- `--server` — gRPC 后端地址（默认 `localhost:50051`）
- `--json` — 输出 JSON 格式

---

## 3. HTTP 端点一览

| 方法 | 路径 | 请求体示例 | 说明 |
|---|---|---|---|
| POST | `/teamx.proto.TeamX/ListTerminals` | `{"page":1,"page_size":50}` | 终端列表（含 session_id + device_id） |
| POST | `/teamx.proto.TeamX/GetTerminal` | `{"session_id":"<sid>"}` 或 `{"device_id":"<did>"}` | 终端详情 + 硬件 |
| POST | `/teamx.proto.TeamX/GetTerminalHistory` | `{"device_id":"<did>","limit":100}` | 硬件历史 |
| POST | `/teamx.proto.TeamX/DisconnectTerminal` | `{"session_id":"<sid>"}` | 踢断会话 |
| POST | `/teamx.proto.TeamX/BlockTerminal` | `{"device_id":"<did>"}` | 封禁设备 |
| POST | `/teamx.proto.TeamX/UnblockTerminal` | `{"device_id":"<did>"}` | 解封设备 |
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

# Linux 客户端（部署到 VM）
GOOS=linux GOARCH=amd64 go build -o bin/client-linux ./cmd/client/

# 修改 proto 后重新生成
buf generate
```

---

## 5. CLI 验证

> 需要先启动 Server（终端 A）。

### 5.1 启动 Server

终端 A：

```powershell
cd D:\MyProjects\TeamX
# 如使用旧版本数据库（含 client_id 列），需先删除：
# del teamx.db
.\bin\server.exe --port 50051 --db teamx.db
```

**预期输出**：

```
[store] schema migrated (17 tables)
database: teamx.db
TeamX Server listening on :50051
  heartbeat check interval: 10s, timeout: 30s
  max connections: 0 (0=unlimited)
```

### 5.2 启动 Client（Windows 本地测试）

终端 B：

```powershell
cd D:\MyProjects\TeamX
.\bin\client.exe --server localhost:50051 --heartbeat 5s
```

**预期输出**：

```
TeamX Client v0.2.0 starting — server=localhost:50051
  hostname=YOUR-PC os=windows
  heartbeat=5s report=5m0s
[client] registered: session=xxxxxxxx device=f3621daac99299c1 server_time=...
[client] channel opened
```

> Windows 硬件桩导致 device_id 使用 hostname+kernel 降级方案（"小明同学|windows" 的 SHA-256）。
> Linux VM 部署后自动使用完整硬件指纹。

### 5.3 admin list — 终端列表

```powershell
.\bin\admin.exe list
```

**预期输出**：

```
SESSION ID                            DEVICE ID         HOSTNAME   OS       VERSION  STATUS   LAST HEARTBEAT
f7bacec1-5bae-4a96-80fa-98c52b1e280f  f3621daac9929...  小明同学       windows  0.2.0    ONLINE   2026-06-28T08:03:30Z
---
Total: 1 terminals (1 online, 0 offline)
```

> ✅ 表格显示 SESSION ID + DEVICE ID 双列。每设备只显示最新会话。

### 5.4 admin list --status / --json

```powershell
.\bin\admin.exe list --status online
.\bin\admin.exe list --json
```

**预期 JSON 输出**（含新字段）：

```json
{
  "terminals": [
    {
      "session_id": "f7bacec1-5bae-4a96-80fa-98c52b1e280f",
      "device_id": "f3621daac99299c1bdc7e1d2519c89913956abe447e2d261d49290dc9db1d792",
      "hostname": "小明同学",
      "os": "windows",
      "os_version": "",
      "client_version": "0.2.0",
      "online": true,
      "last_heartbeat": "2026-06-28T08:03:30Z",
      "last_seen_at": "2026-06-28T08:03:25Z"
    }
  ],
  "total_count": 1
}
```

### 5.5 admin show — 终端详情

```powershell
# 可用 session_id 或 device_id（64 字符自动识别为 device_id）
.\bin\admin.exe show f7bacec1-5bae-4a96-80fa-98c52b1e280f
.\bin\admin.exe show f3621daac99299c1bdc7e1d2519c89913956abe447e2d261d49290dc9db1d792
```

**预期输出**：

```
Summary:
  Session ID:   f7bacec1-5bae-4a96-80fa-98c52b1e280f
  Device ID:    f3621daac99299c1bdc7e1d2519c89913956abe447e2d261d49290dc9db1d792
  Hostname:     小明同学
  OS:           windows (windows)
  Version:      0.2.0
  Status:       ONLINE
  Last Seen:    2026-06-28T08:03:25Z
  First Seen:   

Hardware: (no report yet)
```

> ✅ 同时展示 Session ID 和 Device ID。

### 5.6 admin history — 硬件历史（按 device_id）

```powershell
.\bin\admin.exe history f3621daac99299c1bdc7e1d2519c89913956abe447e2d261d49290dc9db1d792 --limit 3
```

**预期输出**：硬件快照按时间倒序，支持 `--since` / `--until`。

### 5.7 admin kick — 踢断会话（客户端彻底退出）

```powershell
.\bin\admin.exe kick f7bacec1-5bae-4a96-80fa-98c52b1e280f
```

**预期输出**：

```
✓ kicked
```

**客户端预期日志**：

```
[client] connection failed: rpc error: code = PermissionDenied desc = kicked by admin
[client] fatal error — stopping: rpc error: code = PermissionDenied desc = kicked by admin
client exited: rpc error: code = PermissionDenied desc = kicked by admin
```

> ✅ 客户端收到 PermissionDenied → 视为不可重试错误 → 进程彻底退出，不会自动重连。

### 5.8 admin block — 封禁设备

```powershell
.\bin\admin.exe block f3621daac99299c1bdc7e1d2519c89913956abe447e2d261d49290dc9db1d792
```

**预期输出**：`✓ blocked`

**验证封禁生效**：

```powershell
# 1. 当前会话被踢断（同 kick）
# 2. 尝试重新启动客户端：
.\bin\client.exe --server localhost:50051
```

**客户端预期日志**：

```
[client] register rejected: device is blocked
[client] connection failed: device is blocked
[client] fatal error — stopping: device is blocked
client exited: device is blocked
```

> ✅ Block 后设备无法注册，客户端立即退出。

### 5.9 admin unblock — 解封设备

```powershell
.\bin\admin.exe unblock f3621daac99299c1bdc7e1d2519c89913956abe447e2d261d49290dc9db1d792
```

**预期输出**：`✓ unblocked`

客户端可重新正常注册连接。

### 5.10 错误处理

```powershell
.\bin\admin.exe list --server localhost:9999
```

**预期输出**：

```
Error: rpc error: code = Unavailable desc = ... connection refused
```

退出码非零（`echo %ERRORLEVEL%` → `1`）。

---

## 6. HTTP 网关验证

> 需要先启动 Server（终端 A），再启动 Gateway（终端 B）。

### 6.1 启动 Gateway

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

```powershell
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/ListTerminals ^
  -H "Content-Type: application/json" ^
  -d "{\"page\":1,\"page_size\":50}"
```

**预期输出**：JSON 含 `terminals` 数组（每项含 `session_id` + `device_id`）和 `totalCount`。

### 6.3 GetTerminal

```powershell
rem 按 session_id 查询：
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/GetTerminal ^
  -H "Content-Type: application/json" ^
  -d "{\"session_id\":\"<session-id>\"}"

rem 或按 device_id 查询：
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/GetTerminal ^
  -H "Content-Type: application/json" ^
  -d "{\"device_id\":\"<device-id>\"}"
```

**预期输出**：JSON 含 `summary`（含 `session_id` + `device_id`）+ `latestHardware`。

### 6.4 GetTerminalHistory

```powershell
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/GetTerminalHistory ^
  -H "Content-Type: application/json" ^
  -d "{\"device_id\":\"<device-id>\",\"limit\":2}"
```

**预期输出**：JSON 含 `snapshots` 数组。

### 6.5 DisconnectTerminal

```powershell
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/DisconnectTerminal ^
  -H "Content-Type: application/json" ^
  -d "{\"session_id\":\"<session-id>\"}"
```

**预期输出**：`{"ok":true,"message":"kicked"}` 或 `{"message":"session not found or offline"}`。

### 6.6 BlockTerminal / UnblockTerminal

```powershell
rem Block（按 device_id）：
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/BlockTerminal ^
  -H "Content-Type: application/json" ^
  -d "{\"device_id\":\"<device-id>\"}"
rem → {"ok":true,"message":"blocked"}

rem Unblock（按 device_id）：
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/UnblockTerminal ^
  -H "Content-Type: application/json" ^
  -d "{\"device_id\":\"<device-id>\"}"
rem → {"ok":true,"message":"unblocked"}
```

### 6.7 CORS 预检

```powershell
curl -s -I -X OPTIONS http://localhost:8080/teamx.proto.TeamX/ListTerminals ^
  -H "Origin: http://localhost:5173" ^
  -H "Access-Control-Request-Method: POST"
```

**预期响应头**：

```
HTTP/1.1 204 No Content
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: POST, GET, OPTIONS
Access-Control-Allow-Headers: Content-Type, Connect-Protocol-Version, X-User-Agent
```

### 6.8 WebSocket

```bash
# 需要安装: npm i -g wscat 或用浏览器直接测试
wscat -c ws://localhost:8080/ws
```

**预期推送格式**（使用 session_id）：

```json
{"type":"online",  "session_id":"xxx", "hostname":"vm-linux",  "timestamp":"2026-06-28T..."}
{"type":"offline", "session_id":"yyy", "hostname":"win-dev",   "timestamp":"2026-06-28T..."}
```

### 6.9 浏览器最小验证

```js
fetch('http://localhost:8080/teamx.proto.TeamX/ListTerminals', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ page: 1, page_size: 10 })
}).then(r => r.json()).then(console.log)

const ws = new WebSocket('ws://localhost:8080/ws')
ws.onmessage = e => console.log('WS:', JSON.parse(e.data))
```

---

## 7. 通过标准

| # | 验证项 | 预期 | 状态 |
|---|--------|------|------|
| 1 | `go build ./...` | 三个二进制编译通过（含 Linux 交叉编译） | ☐ |
| 2 | `buf generate` | Connect 代码正常生成 | ☐ |
| 3 | `admin list` | 表格含 SESSION ID + DEVICE ID 双列 + total 汇总 | ☐ |
| 4 | `admin list --status online` | 只显示在线终端（每设备只出现一次） | ☐ |
| 5 | `admin list --json` | JSON 含 session_id + device_id | ☐ |
| 6 | `admin show <session-id>` | Summary 含 Session ID + Device ID + Hardware | ☐ |
| 7 | `admin show <device-id>` | 64 字符自动识别为 device_id，同上输出 | ☐ |
| 8 | `admin history <device-id>` | 硬件快照时间线（按 device_id 查询） | ☐ |
| 9 | `admin history --since/--until` | 时间范围过滤生效 | ☐ |
| 10 | `admin kick <session-id>` | 在线会话被踢断；客户端收到 PermissionDenied，进程退出 | ☐ |
| 11 | `admin kick <session-id>`（离线） | 返回 "session not found or offline" | ☐ |
| 12 | `admin block <device-id>` | ✓ blocked；客户端被踢 + 退出 + 重连被拒 | ☐ |
| 13 | `admin unblock <device-id>` | ✓ unblocked；客户端可重新注册 | ☐ |
| 14 | Client 设备指纹 | 同一机器 device_id 恒定；重连不换 device_id | ☐ |
| 15 | Client 设备指纹缓存 | `~/.teamx/device_id` 文件持久化，重启不重新计算 | ☐ |
| 16 | 连接失败错误处理 | 错误只输出一次，退出码非零 | ☐ |
| 17 | HTTP ListTerminals | curl POST → JSON 含 session_id + device_id | ☐ |
| 18 | HTTP GetTerminal | 支持 session_id 和 device_id 两种查询方式 | ☐ |
| 19 | HTTP GetTerminalHistory | curl POST → snapshots[]（按 device_id） | ☐ |
| 20 | HTTP DisconnectTerminal | curl POST → ok/message（按 session_id） | ☐ |
| 21 | HTTP BlockTerminal | curl POST → blocked（按 device_id） | ☐ |
| 22 | HTTP UnblockTerminal | curl POST → unblocked（按 device_id） | ☐ |
| 23 | CORS preflight (OPTIONS) | 204 + Allow-Origin / Allow-Methods / Allow-Headers | ☐ |
| 24 | WebSocket /ws 连接 | 连接成功，上下线时收到 JSON 推送（含 session_id） | ☐ |
| 25 | CLI 与 Gateway 共存 | `admin list` 和 `admin serve` 在同一二进制中正常 | ☐ |
| 26 | Linux 跨主机 | Linux VM Client 上报真实 device_id + 硬件数据 | ☐ |

全部勾选 = **Phase 4a 完成** 🎉

---

## 8. 已知限制

| 限制 | 说明 | 计划 |
|---|---|---|
| Windows 设备指纹使用降级方案 | 当前用 hostname+kernel 计算 SHA-256，非真正硬件指纹 | Phase 10 加 WMI 采集 |
| WebSocket 轮询 | 每 5 秒全量查询状态变化，非实时推送 | Phase 5 可由 Server 主动推送事件 |
| 无认证 | HTTP 网关和 CLI 无需鉴权 | Phase 11 加 JWT/RBAC |
| 无 HTTPS | Gateway 仅 HTTP，无 TLS | Phase 11 加 TLS |
| CORS 默认 `*` | 生产环境不安全 | 生产改为具体 origin |
| Connect 流式未暴露 | Register / Channel / TransferFile 不通过 HTTP 暴露 | 仅限 gRPC |
| 旧数据库不兼容 | `client_id` 列已改为 `session_id`，旧 teamx.db 需重建 | 开发阶段，无迁移脚本 |

---

## 9. 关键行为变更（相对于初始设计）

| 变更 | 原设计 | 最终实现 | 原因 |
|---|---|---|---|
| 终端标识 | 单一 `client_id`（随机 UUID） | `device_id`（硬件指纹）+ `session_id`（会话 UUID） | 区分设备身份与会话，支持按设备封禁 |
| 封禁粒度 | 按 hostname | 按 device_id | hostname 可随意修改，device_id 硬件绑定 |
| Kick 行为 | 客户端秒重连 | 客户端收到 PermissionDenied → 退出 | 避免"踢不断"的假象 |
| Block 行为 | 仅标记 blocked，不主动踢 | 踢断所有会话 + 阻止后续注册 | 即时生效 |
| 心跳/离线索引 | 按 client_id（session） | 按 session_id（不变） | — |
| 硬件报告索引 | 按 client_id | 按 device_id（同设备多次上报可追溯） | — |
