# Phase 5 — 命令下发与控制验证手册

> 验证目标：Server → Client 命令链路完整闭环，支持同步/异步执行与结果回传，
> 命令队列串行化，以及 Restart/Shutdown 等生命周期控制。
>
> Phase 5 分为三个子阶段：
> - **5.1** — Proto 命令模型 + SendCommand/GetCommandLog RPC + Client 执行器
> - **5.2** — 命令队列（串行消费 + 背压 + 离线保护）
> - **5.3** — Panic 隔离 + Restart + Shutdown 处理器

---

## 1. 架构概览

```
Admin CLI / HTTP                     Server (:50051)                 Client
═══════════════                       ═══════════════                 ══════

admin cmd <did> RUN_SCRIPT ──────►  SendCommand RPC
  cmd=uptime                            │
                                        ├─ 校验 device_id/type
                                        ├─ 查终端 + 在线检查
                                        ├─ SaveCommandLog(Pending)
                                        ├─ 非阻塞入队 ──► CmdQueue (chan, cap=32)
                                        └─ 返回 "queued"

                              ┌─ commandConsumer goroutine ─┐
                              │  for cmd := range CmdQueue: │
                              │    stream.Send(Command)     │
                              │    UpdateStatus(Sent)       │
                              │    go watchTimeout(30s)     │
                              └─────────────────────────────┘
                                          │
                                          │  Channel Stream (bidi)
                                          ▼
                              ┌─ recvLoop ──────────────────┐
                              │  dispatchCommand()          │
                              │    ├─ Panic recover         │
                              │    ├─ CollectNow → 立即上报 │
                              │    ├─ RunScript  → exec.Cmd │
                              │    ├─ Restart    → 软重连   │
                              │    └─ Shutdown   → 优雅退出 │
                              │  sendCommandResult()        │
                              └─────────────────────────────┘
                                          │
                              ◄── CommandResult (Executing/Completed/Failed)
                                          │
                              handleCommandResult()
                                └─ UpdateCommandResult() → command_logs 表

admin cmdlog <did> ─────────────► GetCommandLog RPC
                                    └─ SQL: JOIN command_logs + terminals
```

### 1.1 命令生命周期状态机

```
SendCommand ──► Pending ──► 入队成功
                                 │
                      consumer 消费 ──► Sent ──► Client 收到
                                                     │
                                               Executing ──► Completed
                                                           ├─ Failed
                                                           └─ Timeout (30s 超时)
```

### 1.2 命令类型一览

| CommandType | 实现阶段 | 说明 |
|---|---|---|
| `COLLECT_NOW` (1) | 5.1 | 立即触发硬件上报 |
| `RUN_SCRIPT` (2) | 5.1 | 执行 shell 命令（跨平台 sh/cmd） |
| `KILL_PROCESS` (3) | Phase 6 | 插件化进程终止 |
| `UPDATE_CONFIG` (4) | Phase 6 | 插件化配置更新 |
| `UPGRADE` (5) | Phase 9 | 在线升级 |
| `RESTART` (6) | 5.3 | 软重启（取消会话 → 无退避重连） |
| `SHUTDOWN` (7) | 5.3 | 优雅退出 |

---

## 2. 新增/修改文件

| 文件 | 操作 | 说明 |
|---|---|---|
| `internal/proto/teamx.proto` | 修改 | 新增 `CommandType` 枚举、`SendCommandRequest/Response`、`GetCommandLogRequest/Response`、`CommandLogEntry`、2 个 RPC |
| `internal/proto/*.pb.go` | 重新生成 | `buf generate` 产出枚举、序列化、gRPC/Connect stub |
| `internal/server/command.go` | **新建** | `SendCommand`（入队逻辑）+ `GetCommandLog` + `commandConsumer`（串行消费+超时启动）+ `handleCommandResult`（持久化） |
| `internal/server/connection.go` | 修改 | `ClientConn` 加 `CmdQueue chan`；`GetStream()` 线程安全方法；`cmdQueueCapacity=32` |
| `internal/server/server.go` | 修改 | Channel handler 启动 `commandConsumer` goroutine；退出时等待消费完成 |
| `internal/server/store/store.go` | 修改 | Store 接口新增 4 个命令日志方法 + `CommandLogEntry` 结构体 |
| `internal/server/store/command.go` | **新建** | SQLite 命令日志 CRUD（SaveCommandLog/UpdateCommandResult/UpdateCommandStatus/GetCommandLog） |
| `internal/server/store/schema.go` | 已有表 | `command_logs` 表 Phase 3 已建（DDL 含 IF NOT EXISTS） |
| `internal/client/executor.go` | **新建** | 命令分发器（panic 隔离）+ `handleCollectNow` + `handleRunScript` + `handleRestart` + `handleShutdown` |
| `internal/client/client.go` | 修改 | `recvLoop` 接入 `dispatchCommand()`；新增 `cancelSession`/`restartRequested`/`shutdownRequested` 字段；`Run()` 检查 shutdown 标志；`connect()` 检测 restart 返回 nil |
| `cmd/admin/commands.go` | 修改 | 新增 `cmd` 子命令（SendCommand）+ `cmdlog` 子命令（GetCommandLog） |
| `cmd/admin/output.go` | 修改 | 新增 `printCommandLog` + `printCommandResult` 格式化 |
| `cmd/admin/main.go` | 修改 | 注册 `cmd` + `cmdlog` 子命令 |

---

## 3. 编译

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

## 4. CLI 验证

> 需要先启动 Server（终端 A），再启动 Client（终端 B 或 VM）。

### 4.1 启动 Server

终端 A：

```powershell
cd D:\MyProjects\TeamX
.\bin\server.exe
```

**预期输出**：

```
[store] schema migrated (17 tables)
database: teamx.db
TeamX Server listening on :50051
  heartbeat check interval: 10s, timeout: 30s
  max connections: 0 (0=unlimited)
```

### 4.2 启动 Client（Linux VM）

```bash
# 部署
scp bin/client-linux yqz@192.168.235.132:/home/yqz/client-linux

# 启动
ssh yqz@192.168.235.132 "/home/yqz/client-linux --server 192.168.235.1:50051"
```

**预期输出**：

```
TeamX Client v0.2.0 starting — server=192.168.235.1:50051
  hostname=Ubuntu-24 os=linux
  heartbeat=10s report=5m0s
[client] registered: session=xxxxxxxx device=e4c6d9b2cf7d4695 server_time=...
[client] channel opened
```

### 4.3 admin cmd — 基本命令

#### 4.3.1 RUN_SCRIPT（脚本执行）

```powershell
.\bin\admin.exe cmd <device-id> RUN_SCRIPT cmd=uptime
```

**预期输出**：

```
✓ queued
  command_id: b014fe80-6b4e-4f07-870e-1e70ba9ee8e7
```

3 秒后查结果：

```powershell
.\bin\admin.exe cmdlog --limit 1 <device-id>
```

**预期输出**：

```
COMMAND ID                            TYPE        STATUS     EXIT  CREATED               STDOUT
b014fe80-6b4e-4f07-870e-1e70ba9ee8e7  RUN_SCRIPT  Completed  0     2026-07-01T15:34:27Z   23:34:32 up 6:47,  4 users,  load average: 0.08...
---
1 entries
```

#### 4.3.2 COLLECT_NOW（立即上报）

```powershell
.\bin\admin.exe cmd <device-id> COLLECT_NOW
```

**预期输出**：

```
✓ queued
  command_id: d81123c7-828a-4cc2-880c-aefe79328b20
```

Server 日志出现新 hardware report；`cmdlog` 显示 `Completed, stdout="hardware report triggered"`。

#### 4.3.3 RESTART（软重启）

```powershell
.\bin\admin.exe cmd <device-id> RESTART
```

**预期输出**：

```
✓ queued
  command_id: c2463fd3-adcd-4d7f-8b78-2dfca3c5a5bd
```

**验证**：`admin list` 显示 Client 获得新 `session_id`（device_id 不变），且 RESTART 命令 status=Completed。

```powershell
# 重启前
.\bin\admin.exe list --status online
# SESSION ID: 53da6943-...

# 发送 RESTART 后等待 5 秒
.\bin\admin.exe list --status online
# SESSION ID: e55b4c23-...  ← 新 session，同 device

# 验证命令状态
.\bin\admin.exe cmdlog --limit 1 <device-id>
# RESTART  Completed  0  ...  restarting
```

#### 4.3.4 SHUTDOWN（优雅退出）

```powershell
.\bin\admin.exe cmd <device-id> SHUTDOWN
```

**预期输出**：

```
✓ queued
  command_id: 111835e2-f122-4b0f-8689-e22250af5b2b
```

**验证**：`cmdlog` 显示 `SHUTDOWN Completed, stdout="shutting down"`；VM 上 `ps aux | grep client-linux` 无进程。

#### 4.3.5 不支持的设备（离线/不存在）

```powershell
# 不存在的设备
.\bin\admin.exe cmd 0000000000000000000000000000000000000000000000000000000000000001 COLLECT_NOW
# → ✗ device not found

# 离线设备（先 pkill client-linux）
.\bin\admin.exe cmd <device-id> COLLECT_NOW
# → ✗ terminal is offline
```

### 4.4 admin cmd — 超时

```powershell
.\bin\admin.exe cmd --timeout 3 <device-id> RUN_SCRIPT cmd="sleep 10; echo done"
```

**预期**：3 秒后 `cmdlog` 显示 `RUN_SCRIPT Timeout, exit_code=-1, error_message="command timed out"`。

### 4.5 admin cmd — 复杂脚本

```powershell
.\bin\admin.exe cmd <device-id> RUN_SCRIPT cmd="ls -la /tmp"
```

**预期**：`cmdlog` stdout 字段包含 `/tmp` 目录的完整文件列表。

### 4.6 admin cmdlog — 命令历史

```powershell
# 按 device_id 查询
.\bin\admin.exe cmdlog <device-id>

# 按 session_id 查询
.\bin\admin.exe cmdlog <session-id>

# 限制条数 + JSON 输出
.\bin\admin.exe --json cmdlog --limit 5 <device-id>
```

**预期输出**（表格模式）：

```
COMMAND ID                            TYPE         STATUS     EXIT  CREATED               STDOUT
d81123c7-...                          COLLECT_NOW  Completed  0     2026-07-01T...        hardware report triggered
b014fe80-...                          RUN_SCRIPT   Completed  0     2026-07-01T...         23:34:32 up 6:47...
c2463fd3-...                          RESTART      Completed  0     2026-07-01T...        restarting
111835e2-...                          SHUTDOWN     Completed  0     2026-07-01T...        shutting down
39f2f124-...                          RUN_SCRIPT   Timeout    -1    2026-07-01T...
---
5 entries
```

**JSON 输出**（`--json` 模式）含完整字段：`command_id`、`session_id`、`device_id`、`type`（枚举值）、`status`、`exit_code`、`stdout`、`stderr`、`error_message`、`created_at`、`started_at`、`finished_at`。

### 4.7 admin cmd — 帮助信息

```powershell
.\bin\admin.exe cmd --help
```

**预期输出**包含可用命令类型列表：`COLLECT_NOW`、`RUN_SCRIPT`、`RESTART`、`SHUTDOWN`。

---

## 5. HTTP 网关验证

> 需要先启动 Server + Gateway（`admin serve`）。

### 5.1 启动 Gateway

```powershell
cd D:\MyProjects\TeamX
.\bin\admin.exe serve --http-port 8080
```

### 5.2 SendCommand（HTTP）

```powershell
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/SendCommand ^
  -H "Content-Type: application/json" ^
  -d "{\"device_id\":\"<device-id>\",\"type\":2,\"params\":{\"cmd\":\"uptime\"}}"
```

**预期输出**：

```json
{"ok":true,"commandId":"xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx","message":"queued"}
```

> `"type":2` = `COMMAND_TYPE_RUN_SCRIPT`（枚举序号对应 proto 定义）。

### 5.3 GetCommandLog（HTTP）

```powershell
curl -s -X POST http://localhost:8080/teamx.proto.TeamX/GetCommandLog ^
  -H "Content-Type: application/json" ^
  -d "{\"device_id\":\"<device-id>\",\"limit\":5}"
```

**预期输出**：JSON 含 `entries` 数组，每项含 `commandId`、`type`、`status`、`stdout` 等。

---

## 6. 队列验证

### 6.1 串行顺序保证

```powershell
for i in 1 2 3 4 5; do
  .\bin\admin.exe cmd <device-id> RUN_SCRIPT cmd="echo seq-$i"
done
```

**预期**：`cmdlog` 显示 seq-1 到 seq-5 **按顺序**执行。`created_at` 时间戳单调递增，stdout 输出依次为 seq-1, seq-2, ...。

### 6.2 队列满保护

极速发送大量命令（超过 32 容量）：

```powershell
# 先发一个阻塞命令占用队列头
.\bin\admin.exe cmd <device-id> RUN_SCRIPT cmd="sleep 60"
# 快速发 35 个后续命令
for /l %i in (1,1,35) do .\bin\admin.exe cmd <device-id> RUN_SCRIPT cmd="echo fast-%i"
```

**预期**：当队列满时（超过 32 个等待），后续命令返回：

```
✗ command queue full — retry later
```

### 6.3 离线终端无队列泄漏

终端离线后发送命令：

```powershell
.\bin\admin.exe cmd <device-id> RUN_SCRIPT cmd=uptime
# → ✗ terminal is offline
```

终端重新上线后，之前的 Pending 命令不会自动重放（Phase 5 设计如此，离线命令即时失败）。

---

## 7. 全链路验证场景

### 场景 A：运维脚本执行

1. 确认终端在线：`admin list --status online`
2. 发送磁盘检查：`admin cmd <did> RUN_SCRIPT cmd="df -h"`
3. 查询结果：`admin cmdlog --limit 1 <did>`
4. 预期：stdout 含磁盘使用情况

### 场景 B：触发硬件采集

1. 发送：`admin cmd <did> COLLECT_NOW`
2. 查询硬件：`admin show <did>`
3. 预期：`LatestHardware` 字段更新为最新时间戳

### 场景 C：批量终端升级（前奏）

1. 对 N 个在线终端发送 `RUN_SCRIPT cmd="apt update -qq"`
2. 逐一检查 cmdlog 的 exit_code
3. 预期：全部 Completed，exit_code=0

### 场景 D：远程重启终端

1. 记下当前 session_id
2. 发送：`admin cmd <did> RESTART`
3. 等待 5 秒
4. `admin list --status online` 显示新 session_id（device_id 相同）
5. RESTART 命令 status=Completed

### 场景 E：远程关闭终端

1. 发送：`admin cmd <did> SHUTDOWN`
2. 3 秒后 `admin list --status online` 无该终端
3. VM 上 `ps aux | grep client-linux` 无进程
4. SHUTDOWN 命令 status=Completed

---

## 8. 异常场景验证

### 8.1 Panic 隔离

在 `handleCollectNow` 中临时插入 `panic("test")` 代码，发送 COLLECT_NOW 命令：

**预期**：Client 日志出现 `[executor] PANIC in handler: ... panic=test`，`cmdlog` 显示 `Failed, error_message="handler panic: test"`。Client 不会崩溃，recvLoop 继续正常处理后续命令。

### 8.2 超时命令

发送 `RUN_SCRIPT cmd="sleep 60"` 带 `--timeout 3`：

**预期**：3 秒后 status=Timeout，exit_code=-1。Server 和 Client 两侧超时检测均生效。

### 8.3 不存在的脚本

发送 `RUN_SCRIPT cmd="nonexistent_command_xyz"`：

**预期**：status=Failed，exit_code≠0，stderr 含 "command not found" 或类似错误。

### 8.4 空参数命令

发送 `RUN_SCRIPT` 不带 `cmd` 参数：

**预期**：status=Failed，error_message="missing required param: cmd"。

---

## 9. 通过标准

| # | 验证项 | 预期 | 状态 |
|---|--------|------|------|
| 1 | `go build ./...` | 三个二进制编译通过（含 Linux 交叉编译） | ☐ |
| 2 | `buf generate` | 枚举 + 新 RPC stub 正常生成 | ☐ |
| 3 | `admin cmd <did> RUN_SCRIPT cmd=uptime` | queued → Completed, stdout 含 uptime | ☐ |
| 4 | `admin cmd <did> COLLECT_NOW` | queued → Completed, 硬件报告已触发 | ☐ |
| 5 | `admin cmd <did> RESTART` | queued → Completed, Client 获得新 session_id | ☐ |
| 6 | `admin cmd <did> SHUTDOWN` | queued → Completed, Client 进程退出 | ☐ |
| 7 | `admin cmd <did> RUN_SCRIPT`（空参数） | Failed, "missing required param: cmd" | ☐ |
| 8 | `admin cmd --timeout 3 <did> RUN_SCRIPT cmd="sleep 10"` | 3 秒后 Timeout, exit_code=-1 | ☐ |
| 9 | `admin cmd <nonexistent> COLLECT_NOW` | ✗ device not found | ☐ |
| 10 | `admin cmd <did> COLLECT_NOW`（离线） | ✗ terminal is offline | ☐ |
| 11 | `admin cmdlog <did>` | 命令历史表格，含所有字段 | ☐ |
| 12 | `admin --json cmdlog <did>` | JSON 输出含完整字段 | ☐ |
| 13 | 5 个串行 RUN_SCRIPT | stdout seq-1 到 seq-5 按顺序 | ☐ |
| 14 | 极速发送超出队列容量 | 返回 "command queue full — retry later" | ☐ |
| 15 | Panic 隔离 | handler panic → Failed 结果，Client 不崩溃 | ☐ |
| 16 | HTTP SendCommand | curl POST → JSON `{"ok":true,"commandId":"..."}` | ☐ |
| 17 | HTTP GetCommandLog | curl POST → JSON `{"entries":[...]}` | ☐ |
| 18 | 命令状态机完整 | Pending → Sent → Executing → Completed/Failed/Timeout | ☐ |
| 19 | Linux 跨主机 | Linux VM Client 执行脚本 + 回传结果正确 | ☐ |
| 20 | cmdlog 按 device_id | JOIN 查询返回正确的 device_id | ☐ |
| 21 | cmdlog 按 session_id | 过滤正确 | ☐ |

全部勾选 = **Phase 5 完成** 🎉

---

## 10. 已知限制

| 限制 | 说明 | 计划 |
|---|---|---|
| 离线命令不重放 | 终端离线时命令即时失败，不会在重新上线后自动重试 | 后续版本可加 "上线后扫 Pending 命令重发" |
| 命令队列容量固定 | 每终端 32 条，不可配置 | 可改为 `Config` 参数或 Server flag |
| 超时覆盖问题 | 客户端在超时临界点返回结果时，Timeout 状态可能与 Completed 竞态 | 低概率，200ms flush 延迟已解决绝大多数情况 |
| 无命令优先级 | 所有命令 FIFO，紧急命令无法插队 | 可加优先级队列（Phase 6+） |
| 无命令审批 | 所有 admin 可发任意命令，无审批流程 | Phase 11 RBAC |
| Restart 瞬间断连 | 200ms flush 延迟会导致极短暂离线 | 可接受（重连 < 1s） |
| Windows 脚本用 cmd.exe | `RUN_SCRIPT` 在 Windows 上自动使用 `cmd /c` | sh → cmd 语法差异需注意 |
| KillProcess/UpdateConfig/Upgrade 未实现 | 返回 "unsupported command type" | 分别推迟到 Phase 6/9 |
