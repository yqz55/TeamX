# Phase 2 — 终端硬件信息采集验证手册

> 验证目标：Client 采集 CPU/内存/磁盘/网卡/主板信息，通过 gRPC Channel 上报到 Server，SHA-256 去重。

---

## 1. 架构概览

```
Client (Linux)                                    Server
══════════════                                    ══════

collector.Collector
  └── CollectHardware()
        ├── /proc/cpuinfo      → CPUInfo
        ├── /proc/meminfo      → MemoryInfo
        ├── /proc/mounts
        │    + syscall.Statfs  → DiskInfo[]
        ├── net.Interfaces()   → NetInfo[]
        └── /sys/class/dmi/id/ → BIOSInfo + MotherboardInfo

reportLoop (300s)
  └── CollectHardware → SHA-256 vs cache → changed?
       ├── unchanged → skip
       └── changed ────→ ReportRequest{HardwareInfo}
                              │
                    Channel stream.Send ───→ handleReport()
                                              type-switch → 结构化日志
                                              (Phase 3: 落库)
```

### 新增/修改文件

| 文件 | 说明 |
|---|---|
| `internal/client/collector/collector.go` | `Collector` 结构体 + `CollectHardware()` 入口 |
| `internal/client/collector/hardware_linux.go` | Linux 完整实现 (~230 行) |
| `internal/client/collector/hardware_windows.go` | Windows 桩 (Phase 10 完善) |
| `internal/client/collector/cache.go` | `ReportCache` — SHA-256 去重 |
| `internal/client/report.go` | `reportLoop` goroutine |
| `internal/client/client.go` | `Config.ReportInterval`(默认 300s)、启动 `reportLoop` |
| `cmd/client/main.go` | `-report-interval` flag |
| `internal/server/server.go` | `handleReport` type-switch 解包 `HardwareInfo` |

### 关键配置

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-report-interval` | 300s (5 min) | 硬件采集间隔，传 0 禁用 |
| `-heartbeat` | 10s | 心跳间隔（不变） |

---

## 2. 编译

```bash
cd D:\MyProjects\TeamX
export GOPROXY=https://goproxy.cn,direct

# Windows Server
go build -o bin/server.exe ./cmd/server/

# Windows Client（本地验证，硬件信息为桩）
go build -o bin/client.exe ./cmd/client/

# Linux Client（VM 验证，真实硬件数据）
GOOS=linux GOARCH=amd64 go build -o bin/client-linux ./cmd/client/
```

---

## 3. Windows 本地验证（硬件桩）

> Client 的 Windows 采集为占位桩，CPU/Memory 返回空结构体，Disk/Net/BIOS/Motherboard 返回 nil。
> 此步骤仅验证 **上报链路通畅**。

### 3.1 启动 Server

打开 **终端 A**：

```powershell
cd D:\MyProjects\TeamX
.\bin\server.exe
```

**预期输出**：

```
TeamX Server listening on :50051
  heartbeat check interval: 10s, timeout: 30s
```

### 3.2 启动 Client

打开 **终端 B**：

```powershell
cd D:\MyProjects\TeamX
.\bin\client.exe --report-interval 30s
```

> `--report-interval 30s` 缩短上报间隔，方便快速验证。

**预期输出**：

```
TeamX Client v0.2.0 starting — server=localhost:50051
  hostname=YOUR-PC os=windows
  heartbeat=10s report=30s
[client] registered: id=<uuid> server_time=2026-06-14T...
[client] channel opened
```

### 3.3 验证上报

**等待 ≤30 秒**，观察：

**终端 B（Client）**：

```
[client] hardware report sent: id=<uuid> cpu= cores=0 mem=0MB
```

**同时终端 A（Server）**：

```
[report] hardware: client=<uuid> report_id=<uuid> cpu= cores=0/0 arch=amd64 mem=0MB/0MB disks=0 nets=0 bios=false mb=false
```

> ✅ **上报链路验证通过**

### 3.4 验证去重

继续等待 **60 秒**（2 个上报周期），确认 Client 只出现 **1 条** report sent 日志（第 1 个周期发送，第 2 个周期因未变化跳过）。

> ✅ **去重验证通过**

### 3.5 验证重连不复报

1. 终端 B `Ctrl+C` 关闭 Client
2. 重新启动 Client：`.\bin\client.exe --report-interval 30s`
3. 等待 30s

观察 Client 日志：**不会**立即出现第 2 条 report，因为 `ReportCache` 存活，硬件数据未变。

> ✅ **重连去重验证通过**

---

## 4. 跨主机验证（Win Server + Linux Client，推荐）

> 这是**主要验证场景**——Windows 主机跑 Server，Linux VM 跑 Client，采集真实硬件数据。

### 4.1 获取 Windows 主机 IP

| 虚拟化方案 | VM 中访问主机的地址 | 确认方法 |
|---|---|---|
| **WSL2** | `$(cat /etc/resolv.conf \| grep nameserver \| awk '{print $2}')` | 执行左边命令 |
| **VirtualBox NAT** | `10.0.2.2` | 固定地址 |
| **VirtualBox 桥接** | 主机在 LAN 的 IP | `ipconfig` 找 IPv4 |
| **VMware NAT** | 网关地址，通常 `192.168.x.1` | `ip route show default` |

先用 `curl -v telnet://<ip>:50051` 确认可达，如超时说明防火墙拦截，参考 `phase1-verification.md` 3.2 节放行。

### 4.2 启动 Windows Server

```powershell
cd D:\MyProjects\TeamX
.\bin\server.exe
```

### 4.3 拷贝 Client 到 VM

```bash
scp bin/client-linux your-user@<vm-ip>:/home/your-user/
```

### 4.4 启动 Linux Client

```bash
ssh your-user@<vm-ip>
chmod +x client-linux

# 30s 间隔方便验证
./client-linux --server <windows-ip>:50051 --report-interval 30s
```

**预期输出**：

```
TeamX Client v0.2.0 starting — server=<windows-ip>:50051
  hostname=<vm-hostname> os=linux
  heartbeat=10s report=30s
[client] registered: id=<uuid> server_time=2026-06-14T...
[client] channel opened
[client] hardware report sent: id=<uuid> cpu=Intel(R) Core(TM)... cores=8 mem=15758MB
```

### 4.5 验证 Server 收到真实数据

**同时 Windows Server 终端**应输出：

```
[register] client registered: id=<uuid> hostname=<vm-hostname> os=linux version=0.2.0
[channel] stream opened: client=<uuid>
[report] hardware: client=<uuid> report_id=<uuid> cpu=Intel(R) Core(TM) i7-... cores=8/16 arch=amd64 mem=1234MB/15758MB disks=2 nets=3 bios=true mb=true
```

### 4.6 验证数据准确性

在 Linux VM 中手动对比：

```bash
# CPU 型号
cat /proc/cpuinfo | grep "model name" | head -1

# CPU 核心数
cat /proc/cpuinfo | grep "cpu cores" | head -1

# 内存
free -m | head -2

# 磁盘
df -h --type ext4 --type xfs --type btrfs 2>/dev/null
# （Client 只上报真实文件系统类型，tmpfs/snap/overlay 等被过滤）

# 主板
cat /sys/class/dmi/id/board_vendor 2>/dev/null
cat /sys/class/dmi/id/board_name 2>/dev/null
```

**对比规则**：

| 采集项 | Server 日志字段 | 对照命令 |
|---|---|---|
| CPU 型号 | `cpu=...` | `cat /proc/cpuinfo \| grep "model name"` |
| CPU 核心/线程 | `cores=N/M` | `cpu cores` / `siblings` |
| 总内存 | `mem=.../totalMB` | `free -m` 中 Mem: total |
| 磁盘数 | `disks=N` | `df -h --type ext4 --type xfs` |
| 网卡数 | `nets=N` | `ip link show \| grep -c ": <"` |
| BIOS 有值 | `bios=true` | `cat /sys/class/dmi/id/bios_vendor` 存在 |

> ✅ **数据准确性验证通过**

### 4.7 验证去重

继续等待 **≥60 秒**（2 个上报周期）：

- Client 只出现 **1 条** `hardware report sent` → 后续周期无新日志
- Server 只出现 **1 条** `[report] hardware` → 同

> ✅ **跨主机去重验证通过**

---

## 5. 验证心跳不受影响

Server 端持续无 `offline` 标记，心跳保持 10s 周期正常运转。report goroutine 独立运行，不阻塞心跳。

> ✅ **心跳与上报隔离验证通过**

---

## 6. 纯 Linux 验证（可选）

> Server 和 Client 都跑在 Linux VM 上。

```bash
# 编译 Linux Server
GOOS=linux GOARCH=amd64 go build -o bin/server-linux ./cmd/server/

# 拷贝到 VM
scp bin/server-linux bin/client-linux your-user@<vm-ip>:/home/your-user/

# 在 VM 上
ssh your-user@<vm-ip>
chmod +x server-linux client-linux

# 终端 A
./server-linux

# 终端 B
./client-linux --server 127.0.0.1:50051 --report-interval 30s
```

验证项同第 4 节。

---

## 7. 通过标准

| # | 验证项 | 预期 | 状态 |
|---|--------|------|------|
| 1 | 编译 | `go build` 三目标全部通过 | |
| 2 | Client 注册 + Channel | Server 日志 `registered` + `stream opened` | |
| 3 | 首轮上报 | Client 日志 `hardware report sent`，Server 日志 `[report] hardware` | |
| 4 | CPU 准确 | `cpu=` 与 `/proc/cpuinfo` model name 一致 | |
| 5 | 内存准确 | `mem=` 与 `free -m` total 一致（±5%） | |
| 6 | 磁盘数正确 | `disks=` 与实际挂载的真实文件系统数量一致 | |
| 7 | BIOS/主板 | VM/物理机 `bios=true mb=true`，容器内可能 `false` | |
| 8 | 去重 | 第 2 个上报周期无新 report 日志 | |
| 9 | 重连不复报 | `Ctrl+C` 重启 Client 后不立即重复上报 | |
| 10 | 心跳不受影响 | Server 无 `offline` 标记，心跳日志正常 | |
| 11 | Windows 桩 | Windows Client 启动不崩溃（`disks=0 nets=0 bios=false`） | |

全部勾选 = **Phase 2 完成** 🎉
