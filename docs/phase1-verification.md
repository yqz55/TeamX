# Phase 1 — 端到端验证手册

> 验证目标：Server ↔ Client 注册、心跳、离线检测全链路打通。

---

## 1. 编译

在项目根目录（Windows 主机）执行：

```bash
cd D:\MyProjects\TeamX

# Windows 版 Server（跑在主机上）
go build -o bin/server.exe ./cmd/server/

# Windows 版 Client（本地验证用，可选）
go build -o bin/client.exe ./cmd/client/

# Linux 版 Client（跑在 VM 上）
GOOS=linux GOARCH=amd64 go build -o bin/client-linux ./cmd/client/
```

---

## 2. Windows 本地验证

### 2.1 启动 Server

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

### 2.2 启动 Client

打开 **终端 B**：

```powershell
cd D:\MyProjects\TeamX
.\bin\client.exe
```

**预期输出**：

```
TeamX Client v0.2.0 starting — server=localhost:50051
  hostname=YOUR-PC os=windows
[client] registered: id=<uuid> server_time=2026-06-13T...
[client] channel opened
```

**同时终端 A（Server）输出**：

```
[register] client registered: id=<uuid> hostname=YOUR-PC os=windows version=0.2.0
[channel] stream opened: client=<uuid>
```

> ✅ **注册 + Channel 建立验证通过**

### 2.3 确认心跳

保持两个终端运行 **至少 30 秒**，观察：

- Server 终端 **不** 输出 `offline` 标记 → 心跳正常
- 心跳日志默认静默（避免刷屏），不影响验证

> ✅ **心跳保活验证通过**

### 2.4 测试离线检测

1. 在终端 B（Client）按 `Ctrl+C` 关闭客户端
2. 观察终端 A（Server），**等待 ≤30 秒**

Server 的 `HeartbeatChecker` 每 10s 扫描一次，发现 `LastHeartbeat > 30s` 后标记离线。

> ✅ **离线检测验证通过**

---

## 3. 跨主机验证（推荐） Win Server + Linux Client

> 你的 Windows 主机跑 Server，Linux VM 跑 Client，模拟真实部署拓扑。

### 3.1 获取 Windows 主机在 VM 眼中的 IP

不同的虚拟化方案，VM 访问主机的方式不同。先确认你用哪种：

| 虚拟化方案 | VM 中访问主机的地址 | 如何确认 |
|---|---|---|
| **WSL2** | `$(cat /etc/resolv.conf \| grep nameserver \| awk '{print $2}')` | 执行左边命令即可 |
| **VirtualBox NAT** | `10.0.2.2` | 固定地址 |
| **VirtualBox 桥接** | 主机在 LAN 的 IP | PowerShell: `ipconfig` 找 IPv4 |
| **VMware NAT** | 网关地址，通常 `192.168.x.1` | `ip route show default` |
| **Hyper-V / 其他** | 主机在 LAN 的 IP | PowerShell: `ipconfig` 找 IPv4 地址 |

**通用方法**——先 SSH 进 VM，测试能否连上主机：

```bash
# 在 VM 中执行（把 <ip> 换成你猜测的地址）
curl -v telnet://<ip>:50051
# 连接成功 = IP 正确
# 连接超时 = IP 不对，或者防火墙拦了（继续 3.2）
```

### 3.2 开放 Windows 防火墙

Windows 防火墙默认阻止入站连接，需要给 50051 端口放行。

**方法一：图形界面（推荐）**

1. 按 `Win+R` → 输入 `wf.msc` → 回车
2. 左侧点击 **"入站规则"**
3. 右侧点击 **"新建规则..."**
4. 规则类型：**端口** → 下一步
5. 协议：**TCP**，特定本地端口：**50051** → 下一步
6. 操作：**允许连接** → 下一步
7. 配置文件：全部勾选（域/专用/公用） → 下一步
8. 名称：`TeamX Server` → 完成

**方法二：命令行（管理员 PowerShell）**

```powershell
New-NetFirewallRule -DisplayName "TeamX Server" -Direction Inbound -Protocol TCP -LocalPort 50051 -Action Allow
```

验证防火墙规则生效：

```powershell
Get-NetFirewallRule -DisplayName "TeamX Server" | Format-List Enabled,Direction,Action
```

### 3.3 启动 Windows Server

在 Windows 主机上打开 **终端**：

```powershell
cd D:\MyProjects\TeamX
.\bin\server.exe
```

**预期输出**：

```
TeamX Server listening on :50051
  heartbeat check interval: 10s, timeout: 30s
```

> `:50051` 前面没有 IP = 监听所有网卡（0.0.0.0），VM 可以访问。

### 3.4 拷贝 Client 到 Linux VM

```bash
# 在 Windows 终端中执行
scp bin/client-linux your-user@<vm-ip>:/home/your-user/
```

### 3.5 在 Linux VM 上启动 Client

```bash
ssh your-user@<vm-ip>

# 添加执行权限
chmod +x client-linux

# 启动 Client，--server 填 Windows 主机的 IP（见 3.1）
./client-linux --server <windows-ip>:50051
```

**预期输出**：

```
TeamX Client v0.2.0 starting — server=<windows-ip>:50051
  hostname=<vm-hostname> os=linux
[client] registered: id=<uuid> server_time=2026-06-13T...
[client] channel opened
```

**同时 Windows Server 终端输出**：

```
[register] client registered: id=<uuid> hostname=<vm-hostname> os=linux version=0.2.0
[channel] stream opened: client=<uuid>
```

> ✅ **跨主机注册 + Channel 建立验证通过**

### 3.6 确认心跳

保持两个终端运行 **至少 30 秒**，Server 终端无 `offline` 标记。

> ✅ **跨主机心跳保活验证通过**

### 3.7 测试离线检测

1. 在 Linux VM 终端按 `Ctrl+C` 关闭 Client
2. 观察 Windows Server 终端，**等待 ≤30 秒**

Server 的 `HeartbeatChecker` 每 10s 扫描一次，发现 `LastHeartbeat > 30s` 后标记离线。

> ✅ **跨主机离线检测验证通过**

---

## 4. 纯 Linux VM 验证（可选）

> Server 和 Client 都跑在 Linux VM 上，验证 Linux 平台的完整功能。

### 4.1 拷贝文件到 VM

```bash
GOOS=linux GOARCH=amd64 go build -o bin/server-linux ./cmd/server/

scp bin/server-linux bin/client-linux your-user@<vm-ip>:/home/your-user/
```

### 4.2 在 VM 上执行

```bash
ssh your-user@<vm-ip>
chmod +x server-linux client-linux

# 终端 A：启动 Server
./server-linux

# 终端 B：启动 Client（指向本地 Server）
./client-linux --server 127.0.0.1:50051
```

### 4.3 观察

- Server 日志出现 `registered` + `stream opened`
- 保持运行 30s，Server 无 `offline` 标记
- `Ctrl+C` 客户端，30s 内 Server 标记离线

---

## 5. 通过标准

| 场景 | 预期 | 实际 |
|---|---|---|
| Server 启动 | `listening on :50051` | |
| Client 连接 | Server 日志：`registered` | |
| Channel 建立 | Server 日志：`stream opened` | |
| 持续 30s 心跳 | Server 无 offline 标记 | |
| Client 断开 | Server ≤30s 标记 offline | |
| **Linux 同样通过** | 同 Windows | |

全部勾选 = **Phase 1 完成** 🎉
