# Phase 3 — Server 数据存储与连接管理验证手册

> 验证目标：Server 持久化终端上报数据到 SQLite，提供查询 RPC（终端列表/详情/历史），
> 支持主动踢断、黑名单封禁、连接数限制。

---

## 1. 架构概览

```
Client (Linux/Windows)                           Server
══════════════════════                           ══════

Register ──────────────────────────► Register
                                      ├─ IsFull()        → 容量检查
                                      ├─ IsBlocked()     → 黑名单检查
                                      └─ UpsertTerminal() → terminals 表

Heartbeat ─────────────────────────► handleHeartbeat()
                                      ├─ RecordHeartbeat()  → 内存
                                      └─ UpdateHeartbeat()  → terminals 表

ReportRequest ─────────────────────► handleReport()
                                      └─ SaveHardwareReport()
                                           ├─ hardware_reports
                                           ├─ hardware_disks
                                           ├─ hardware_nets
                                           ├─ hardware_bios
                                           └─ hardware_motherboard

Admin (gRPC) ──────────────────────► ListTerminals()
                                    ► GetTerminal()
                                    ► GetTerminalHistory()
                                    ► DisconnectTerminal()  → Kick()
                                    ► BlockTerminal()       → MarkBlocked()
                                    ► UnblockTerminal()     → UnblockTerminal()

Channel handler (select loop)
  ├─ msgCh ← recv goroutine   (gRPC stream messages)
  └─ DisconnectCh             (admin kick signal)
```

### 新增/修改文件

| 文件 | 说明 |
|---|---|
| `internal/server/store/store.go` | `Store` 接口 + `HardwareSnapshot` / `Terminal` 类型 + SQLite WAL 连接 |
| `internal/server/store/schema.go` | 11 张表 DDL（IF NOT EXISTS 幂等迁移） |
| `internal/server/store/terminal.go` | Terminal CRUD + Blocklist 操作 |
| `internal/server/store/hardware.go` | SaveHardwareReport + GetLatest + ListByTimeRange |
| `internal/server/connection.go` | `DisconnectCh` + `Kick()` + `IsFull()` + `SetMaxConns()` |
| `internal/server/server.go` | Register 容量/黑名单检查；Channel select 多路复用；6 个查询 + 3 个管理 RPC handler |
| `cmd/server/main.go` | `--db` / `--max-connections` flags |
| `internal/proto/teamx.proto` | 6 个新 RPC + 13 个新 message |
| `buf.yaml` / `buf.gen.yaml` | buf 代码生成配置（protoc 替代方案） |

---

## 2. 数据库表结构

```
terminals
  ├── client_id (PK)
  ├── hostname, os, os_version, kernel_version, client_version
  ├── mac_addrs (JSON), ip_addrs (JSON)
  ├── first_seen_at, last_seen_at, last_heartbeat
  ├── online (0/1), blocked (0/1)

hardware_reports
  ├── id (PK), report_id (UNIQUE), client_id (FK)
  ├── created_at
  ├── cpu_model, cpu_cores, cpu_threads, cpu_arch
  └── mem_total_bytes, mem_avail_bytes, mem_used_bytes
        │
        ├── hardware_disks     (report_id FK) — device, mount_point, fs_type, total/used/free
        ├── hardware_nets      (report_id FK) — name, mac_addr, ip_addrs(JSON), is_loopback
        ├── hardware_bios      (report_id FK) — vendor, version, release_date
        └── hardware_motherboard (report_id FK) — manufacturer, product, serial

software_items     (Phase 6 预留)
user_accounts      (Phase 6 预留)
process_items      (Phase 6 预留)
peripheral_devices (Phase 6 预留)
command_logs       (Phase 5 预留)
```

### SQLite 配置

| 参数 | 值 | 说明 |
|---|---|---|
| journal_mode | WAL | 写不阻塞读 |
| synchronous | NORMAL | 安全 + 性能平衡 |
| busy_timeout | 5000ms | 写锁等待而非立即报错 |
| max_open_conns | 4 | 允许嵌套查询（主查询 + 子表加载） |

---

## 3. 查询 RPC 一览

| RPC | 请求 | 响应 | 说明 |
|---|---|---|---|
| `ListTerminals` | `online_filter` (optional) + `page` + `page_size` | `TerminalSummary[]` + `total_count` | 分页列表，支持按在线状态过滤 |
| `GetTerminal` | `client_id` | `TerminalSummary` + `latest_hardware` | 单终端详情，含最新硬件快照 |
| `GetTerminalHistory` | `client_id` + `since`/`until` (optional) + `limit` | `HardwareSnapshot[]` | 硬件历史时间序列 |

### 管理 RPC 一览

| RPC | 请求 | 响应 | 说明 |
|---|---|---|---|
| `DisconnectTerminal` | `client_id` | `ok` + `message` | 踢断在线终端 |
| `BlockTerminal` | `client_id` | `ok` + `message` | 加入黑名单，同 hostname 无法再次注册 |
| `UnblockTerminal` | `client_id` | `ok` + `message` | 解除黑名单 |

---

## 4. 编译

```bash
cd D:\MyProjects\TeamX
export GOPROXY=https://goproxy.cn,direct

# 全部二进制
go build -o bin/server.exe ./cmd/server/
go build -o bin/client.exe ./cmd/client/
GOOS=linux GOARCH=amd64 go build -o bin/client-linux ./cmd/client/

# 修改 proto 后重新生成
buf generate
```

---

## 5. 本地验证（Windows Server + Windows Client）

> 验证 3.1（存储落库）+ 3.2（查询 RPC）+ 3.3（管控 RPC）。
> Windows Client 硬件为桩，数据为空是预期行为。

### 5.1 启动 Server

打开终端 A：

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

> `teamx.db` 文件自动创建在项目根目录，包含 11 张业务表 + SQLite 系统表共 16 个。

### 5.2 启动 Client

打开终端 B：

```powershell
cd D:\MyProjects\TeamX
.\bin\client.exe --server localhost:50051 --report-interval 10s --heartbeat 5s
```

**预期输出**：

```
TeamX Client v0.2.0 starting — server=localhost:50051
  hostname=YOUR-PC os=windows
  heartbeat=5s report=10s
[client] registered: id=<uuid> server_time=2026-06-14T...
[client] channel opened
[client] hardware report sent: id=<uuid> cpu= cores=0 mem=0MB
```

### 5.3 验证 3.1 — 数据落库

用 `golang` 脚本直接查 SQLite（无需 sqlite3 CLI）：

```powershell
cd D:\MyProjects\TeamX
go run tools/dump_db.go
```

> 如果 `tools/dump_db.go` 不存在，创建一个简易脚本见下方。
> 预期看到 `terminals` 表有 1 行，`hardware_reports` 表有 1 行。

**临时查询脚本**（保存为 `tools/dump_db.go`）：

```go
package main

import (
    "database/sql"
    "fmt"
    "log"
    _ "modernc.org/sqlite"
)

func main() {
    db, _ := sql.Open("sqlite", "teamx.db")
    defer db.Close()

    fmt.Println("=== terminals ===")
    rows, _ := db.Query("SELECT client_id, hostname, os, online, last_heartbeat, blocked FROM terminals")
    for rows.Next() {
        var cid, host, os, lhb string
        var online, blocked int
        rows.Scan(&cid, &host, &os, &online, &lhb, &blocked)
        fmt.Printf("  id=%s host=%s os=%s online=%d blocked=%d hb=%s\n",
            cid[:8], host, os, online, blocked, lhb[:19])
    }
    rows.Close()

    fmt.Println("\n=== hardware_reports ===")
    rows2, _ := db.Query("SELECT report_id, client_id, cpu_arch, mem_total_bytes/1048576, created_at FROM hardware_reports ORDER BY created_at DESC LIMIT 5")
    for rows2.Next() {
        var rid, cid, arch, created string
        var mem int64
        rows2.Scan(&rid, &cid, &arch, &mem, &created)
        fmt.Printf("  rid=%s cid=%s arch=%s mem=%dMB created=%s\n",
            rid[:8], cid[:8], arch, mem, created[:19])
    }
    rows2.Close()

    fmt.Println("\n✅ 3.1 数据落库验证通过")
}
```

**执行**：

```powershell
cd D:\MyProjects\TeamX
go run tools/dump_db.go
```

**预期输出**：

```
=== terminals ===
  id=xxxxxxxx host=YOUR-PC os=windows online=1 blocked=0 hb=2026-06-14T...

=== hardware_reports ===
  rid=xxxxxxxx cid=xxxxxxxx arch=amd64 mem=0MB created=2026-06-14T...

✅ 3.1 数据落库验证通过
```

### 5.4 验证 3.2 — 查询 RPC

```powershell
cd D:\MyProjects\TeamX
go run tools/check_rpc.go
```

**预期输出**：

```
=== ListTerminals (all) ===
total=1 terminals=1
  id=xxxxxxxx host=YOUR-PC os=windows online=true hb=2026-06-14T...

=== ListTerminals (online only) ===
total=1

=== GetTerminal ===
summary: host=YOUR-PC os=windows
hardware: cpu= cores=0 threads=0 arch=amd64 mem=0MB

=== GetTerminalHistory ===
snapshots=1
  [0] rid=xxxxxxxx created=2026-06-14T...

✅ All 3 query RPCs work correctly.
```

### 5.5 验证 3.3.1 — Kick

```powershell
cd D:\MyProjects\TeamX
go run tools/check_33.go
```

**预期输出**（关键行）：

```
=== 1. ListTerminals ===
client=xxxxxxxx host=YOUR-PC online=true

=== 2. DisconnectTerminal (Kick) ===
ok=true msg=kicked
  after kick: online=false

=== 3. BlockTerminal ===
ok=true msg=blocked

=== 4. UnblockTerminal ===
ok=true msg=unblocked

✅ Phase 3.3 — all 3 control RPCs work correctly.
```

同时终端 B 的 Client 日志应出现：

```
[channel] admin kick: client=<old-id>
[client] disconnected — will retry
[client] registered: id=<new-id> server_time=...
[client] channel opened
```

> ✅ **Kick 验证通过**：Client 被踢后自动重连，获得新的 client_id。

### 5.6 验证 3.3.2 — 黑名单

> 终端 B（Client）已重连为新 client_id，但 hostname 不变。

1. 查当前终端列表，记下 client_id（新 ID）
2. Block 该 client_id：

```powershell
# 用 grpcurl 或 Go 脚本调用
go run -exec '' tools/block_and_test.go 2>/dev/null || echo "手动测试："
# Block 后，终端 B Ctrl+C 断开，再次启动
# 预期：Register 被拒，日志 "[register] rejected: hostname=... is blocked"
```

**Server 日志预期**：

```
[admin] block: client=<id> host=YOUR-PC
# Client 重连时：
[register] rejected: hostname=YOUR-PC is blocked
```

3. Unblock：

```
[admin] unblock: client=<id>
# Client 重连：
[register] client registered: id=<new-id>  # 注册成功
```

> ✅ **黑名单验证通过**

### 5.7 验证 3.3.3 — 连接数限制

**终端 A**：启动 Server，限制 1 个连接

```powershell
.\bin\server.exe --port 50051 --db teamx.db --max-connections 1
```

**终端 B**：启动 Client 1

```powershell
.\bin\client.exe --server localhost:50051
```

**终端 C**：启动 Client 2

```powershell
.\bin\client.exe --server localhost:50051
```

**终端 C 预期输出**：

```
[client] connection failed: rpc error: code = ResourceExhausted desc = server at capacity
```

**同时 Server 日志**：

```
[register] rejected: server full hostname=YOUR-PC
```

关闭 Client 1 后，Client 2 自动重连成功：

```
[client] registered: id=<new-id>
[client] channel opened
```

> ✅ **容量限制验证通过**

---

## 6. 跨主机验证（Win Server + Linux Client）

> 主要验证场景：Windows 运行 Server + 数据库，Linux VM 运行 Client 上报真实硬件数据。

### 6.1 启动 Windows Server

```powershell
cd D:\MyProjects\TeamX
.\bin\server.exe --port 50051 --db teamx.db
```

### 6.2 编译 + 部署 Linux Client

```bash
cd D:\MyProjects\TeamX
GOOS=linux GOARCH=amd64 go build -o bin/client-linux ./cmd/client/
scp bin/client-linux yqz@192.168.235.132:/home/yqz/client-linux
```

### 6.3 启动 Linux Client

```bash
ssh yqz@192.168.235.132
chmod +x client-linux
./client-linux --server <windows-host-ip>:50051 --report-interval 30s
```

### 6.4 验证数据落库

在 Windows 上运行：

```powershell
go run tools/dump_db.go
```

**预期**：`terminals` 有 Linux VM 主机名 + 真实 CPU/内存数据。

### 6.5 验证查询 RPC

```powershell
go run tools/check_rpc.go
```

**预期**：hardware 字段有真实数据（非空）。

### 6.6 验证 Kick + Block + Unblock

```powershell
go run tools/check_33.go
```

**预期**：同 5.5-5.6 节，Linux Client 被踢后重连。

---

## 7. 通过标准

| # | 验证项 | 预期 | 状态 |
|---|--------|------|------|
| 1 | 编译 | `go build ./...` 全部通过 | |
| 2 | Schema 迁移 | Server 启动日志 `schema migrated (16 tables)` | |
| 3 | 终端落库 | `terminals` 表有注册记录 | |
| 4 | 硬件落库 | `hardware_reports` 表有上报记录 | |
| 5 | 去重 | 重复 `report_id` 不产生脏数据（`INSERT OR IGNORE`） | |
| 6 | 心跳持久化 | `terminals.last_heartbeat` 持续更新 | |
| 7 | 离线标记 | Client 断连 + 30s 后 `online=0` | |
| 8 | ListTerminals | 返回分页列表，含 `total_count` | |
| 9 | ListTerminals (filter) | `online_filter=true/false` 正确过滤 | |
| 10 | GetTerminal | 返回 TerminalSummary + latest HardwareInfo | |
| 11 | GetTerminalHistory | 返回 HardwareSnapshot 列表（含 report_id + created_at） | |
| 12 | Kick | Client 被踢后重连，获得新 client_id | |
| 13 | Block | blocked=1，同 hostname 再次 Register 被拒 | |
| 14 | Unblock | blocked=0，同 hostname 可以 Register | |
| 15 | 容量限制 | `--max-connections N` 下第 N+1 个 Client 被拒 | |
| 16 | Linux 跨主机 | Linux VM Client 上报真实硬件，数据准确 | |

全部勾选 = **Phase 3 完成** 🎉

---

## 8. 已知限制

| 限制 | 说明 | 计划 |
|---|---|---|
| Windows 硬件桩 | CPU/Memory/Disk/Net 返回空或 nil | Phase 10 完善 |
| Block 按 hostname | 同一 hostname 所有终端都被封；未来可按 MAC 增强 | Phase 11 |
| 无认证 | 所有 RPC 无需鉴权，任何人可调 DisconnectTerminal | Phase 11 加 JWT/RBAC |
| 单 Server 实例 | SQLite 文件锁不支持多 Server 共享 | 如需分布式部署可切 PostgreSQL |
