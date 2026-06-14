# TODO.md — 基于插件的终端管理系统

## 技术选型建议
- **语言**: Go（并发模型成熟，跨平台编译简单，适合网络服务 + 插件机制）
- **RPC 框架**: gRPC + Protobuf（强类型 IDL，内置流式传输，多语言支持）
- **插件机制**: Go plugin（`.so` 动态加载）或 HashiCorp go-plugin（进程级隔离）
- **管理界面**: React/Vue + gRPC-Web 或 REST API 网关
- **数据库**: SQLite（轻量）或 PostgreSQL（生产环境）

---

## Phase 0 — 项目初始化与环境搭建

### 0.1 项目骨架
- [ ] 初始化 Go module（`go mod init teamx`）
- [ ] 建立目录结构：
  ```
  TeamX/
  ├── cmd/
  │   ├── client/          # 客户端入口
  │   ├── server/          # 服务端入口
  │   └── admin/           # 管理后台入口
  ├── internal/
  │   ├── proto/           # Protobuf 定义
  │   ├── server/          # 服务端核心逻辑
  │   ├── client/          # 客户端核心逻辑
  │   ├── plugin/          # 插件框架
  │   └── common/          # 公共工具
  ├── plugins/             # 内置插件目录
  ├── web/                 # 管理界面前端
  ├── scripts/             # 构建/测试脚本
  └── docs/                # 设计文档
  ```
- [ ] 编写 Makefile（`make build`, `make proto`, `make test`）
- [ ] 配置 `.gitignore`、`.golangci.yml`

### 0.2 通信协议定义（Proto）
- [x] 设计核心 Protobuf 消息结构：
  - `Handshake`（客户端注册/认证）→ HandshakeRequest / HandshakeResponse
  - `Heartbeat`（心跳保活）→ Heartbeat / HeartbeatAck
  - `ReportRequest/Response`（信息上报）→ ReportRequest (oneof: HardwareInfo, SoftwareInfo, UserInfo, ProcessInfo, PeripheralInfo)
  - `CommandRequest/Response`（命令下发与执行）→ Command / CommandResult
  - `FileChunk`（文件传输分片）→ FileChunk / FileTransferRequest / FileTransferResponse / FileData
- [x] 编译 `.proto` → Go 代码（teamx.pb.go + teamx_grpc.pb.go）
- [x] 编写协议文档（字段说明、错误码定义）→ docs/protocol.md

---

## Phase 1 — 核心通信链路（先跑通 Client ↔ Server）

> **目标**: 一个 Go TCP/gRPC server 能接收 client 连接，client 能发心跳，server 能回响应。

### 1.1 服务端基础
- [x] 实现 gRPC server 启动与端口监听
- [x] 实现客户端连接管理器（`ConnectionManager`）：
  - 维护 `map[clientID]*ClientConn`
  - 提供 `Register` / `Unregister` / `Get` 方法
  - 线程安全（`sync.RWMutex`）→ `internal/server/connection.go`

### 1.2 客户端基础
- [x] 实现 gRPC client 拨号连接
- [x] 实现自动重连（指数退避: 1s→2s→4s→...→60s, ±25% jitter）
- [x] 客户端注册流程：连接 → 发送 `Handshake`（hostname, OS, clientVersion）→ 获取 `clientID`

### 1.3 心跳机制
- [x] 客户端定时发送 `Heartbeat`（间隔 10s）
- [x] 服务端定期检查心跳超时（超过 30s 标记离线）
- [ ] 管理界面可看到终端在线/离线状态 → Phase 4

### 1.4 验证
- [x] 手动启动 server、1 个 client，确认注册 + 心跳正常（Windows + Linux 双平台）
- [x] 模拟 client 断网（Ctrl+C），确认超时离线检测正常
- [x] 跨主机验证：Win Server + Linux VM Client 通过

---

## Phase 2 — 终端硬件信息采集（Client 端）

> **目标**: Client 采集本机硬件信息（CPU/内存/磁盘/网卡/主板），并通过 gRPC 上报到 Server。
>
> Phase 2 只实现内置硬件采集。软件/用户/进程/外设作为插件模块推迟到 Phase 6 实现（见 [架构决策](#架构决策-内置-vs-插件)）。

### 架构决策：内置 vs 插件

采集模块按下述三问分类：

> 1. 如果这个功能没了，Agent 还能正常连接服务器并响应基本指令？
> 2. 这个功能是否需要随业务策略频繁更新，而客户端主程序应保持稳定？
> 3. 这个功能是否需要按终端角色、场景差异来选择性启用？

| 模块 | 分类 | Phase | 理由 |
|---|---|---|---|
| **HardwareInfo** | **内置** | Phase 2 | Q1✅ 握手已有轻量指标；Q2❌ 解析逻辑 20 年不变；Q3❌ 所有终端都需要资产清册 |
| **SoftwareInfo** | **插件** | Phase 6 | Q2✅ 合规基线/分类规则持续演进；Q3✅ 服务器/工作站/边缘设备需求不同 |
| **UserInfo** | **插件** | Phase 6 | Q2✅ 审计规则多变（弱口令/过期账户等）；Q3✅ 服务器需审计，IoT 无用户概念 |
| **ProcessInfo** | **插件** | Phase 6 | Q2✅ 监控策略多变（黑白名单/异常检测）；Q3✅ 安全/运维/普通场景关注点不同 |
| **PeripheralInfo** | **插件** | Phase 6 | Q2✅ USB 安全策略独立演进；Q3✅ 涉密环境强需求，云 VM 完全不需要 |

### 2.1 硬件信息采集（`internal/client/collector/`）
- [x] CPU 型号、核心数、逻辑线程数
- [x] 内存总量、可用量、已用量
- [x] 磁盘列表、挂载点、文件系统类型、容量
- [x] 网卡列表、MAC 地址、IP 地址
- [x] 主板/BIOS 信息（可选）

### 2.2 采集调度
- [x] 实现定时采集（`reportLoop` goroutine，可配置间隔，默认 300s）
- [x] 采集数据的本地缓存与去重（`ReportCache`，SHA-256 比对，避免重复上报）

### 2.3 上报到 Server
- [x] 实现 gRPC 流式上报（通过 Channel 发送 `ReportRequest`）
- [x] Server 端解析并打日志（`handleReport` type-switch 解包 HardwareInfo）

### 2.4 验证
- [x] 分别在 Linux 和 Windows 上测试硬件信息采集
- [x] 确认 Server 收到完整上报数据

---

## Phase 3 — Server 数据存储与连接管理

> **目标**: Server 持久化终端上报的数据，支持查询和连接管理。

### 3.1 数据存储
- [ ] 设计数据库表结构（终端表、硬件表、软件表、进程表、用户表、外设表、命令日志表）
- [ ] 实现数据持久化层（`internal/server/store/`）
- [ ] 上报数据写入数据库

### 3.2 终端管理与查询 API
- [ ] 终端列表查询（分页、按状态过滤）
- [ ] 单个终端详情查询（硬件/软件/进程/用户/外设）
- [ ] 终端历史数据查询（时间范围）

### 3.3 连接管理
- [ ] 主动断开指定终端连接
- [ ] 终端黑名单（禁止特定 clientID 连接）
- [ ] 连接数限制与过载保护

### 3.4 验证
- [ ] 启动 server + 3 个 client，确认数据持久化
- [ ] 查询 API 返回正确数据

---

## Phase 4 — 管理界面（Web）

> **目标**: 提供 Web 界面查看终端列表、详情，发送命令。

### 4.1 后端 API 层
- [ ] RESTful API（或 gRPC-Web）封装：
  - `GET /api/terminals` — 终端列表
  - `GET /api/terminals/:id` — 终端详情
  - `POST /api/terminals/:id/command` — 发送命令
  - `GET /api/terminals/:id/hardware` — 硬件信息
  - `GET /api/terminals/:id/software` — 软件信息
  - `GET /api/terminals/:id/processes` — 进程列表
- [ ] WebSocket 端点（终端状态实时推送）

### 4.2 前端页面
- [ ] 终端列表页（表格 + 在线/离线状态标签）
- [ ] 终端详情页（Tab 切换：概览/硬件/软件/进程/用户/外设）
- [ ] 命令发送面板（选择终端 → 输入命令 → 查看执行结果）
- [ ] 实时状态更新（WebSocket 推送在线/离线变化）

### 4.3 前端技术选型与搭建
- [ ] 初始化前端项目（React/Vue + TypeScript）
- [ ] 组件库引入（Ant Design / Element Plus）
- [ ] API 请求封装
- [ ] 路由配置

### 4.4 验证
- [ ] 浏览器访问管理界面，看到终端列表
- [ ] 点击终端查看详情数据
- [ ] 在线/离线状态实时更新

---

## Phase 5 — 命令下发与控制

> **目标**: 管理界面 → Server → Client 的命令链路，支持同步/异步执行与结果回传。

### 5.1 命令模型
- [ ] 定义命令类型枚举（`CollectNow`, `RunScript`, `KillProcess`, `UpdateConfig`, `Upgrade`, `Restart`, `Shutdown`）
- [ ] 命令生命周期状态机（`Pending → Sent → Executing → Completed/Failed/Timeout`）

### 5.2 Server 端命令调度
- [ ] 命令队列管理（每终端独立队列）
- [ ] 命令超时处理（可配置超时时间）
- [ ] 命令执行结果存储与查询

### 5.3 Client 端命令执行
- [ ] 接收命令流（gRPC server-side stream）
- [ ] 命令分发到对应处理器
- [ ] 执行结果回传

### 5.4 验证
- [ ] 管理界面发送 "CollectNow" → client 立即上报 → 界面可见最新数据
- [ ] 发送超时未响应的命令 → 状态标记为 Timeout
- [ ] 并发向 10 个终端发送命令 → 全部正确执行

---

## Phase 6 — 插件系统

> **目标**: 支持动态加载插件扩展客户端功能，无需重新编译客户端。

### 6.1 插件框架设计
- [ ] 定义插件接口（Go interface）：
  ```go
  type Plugin interface {
      Name() string
      Version() string
      Init(config map[string]interface{}) error
      Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error)
      Shutdown() error
  }
  ```
- [ ] 插件元数据定义（`plugin.json`: name, version, author, dependencies）

### 6.2 插件加载器
- [ ] 动态发现（扫描 `plugins/` 目录）
- [ ] 插件加载/卸载
- [ ] 插件依赖解析
- [ ] 插件隔离（错误隔离：单个插件崩溃不影响其他插件和主进程）
- [ ] 插件热重载（检测文件变化自动 reload）

### 6.3 插件管理 API
- [ ] Server 端：插件列表查询、插件下发到终端
- [ ] Client 端：接收插件包、加载/卸载/启用/禁用插件
- [ ] 管理界面：插件管理页面（上传插件、分发插件、查看各终端插件状态）

### 6.4 内置示例插件
- [ ] `hardware-collector` — 硬件采集插件（将 Phase 2 的 HardwareInfo 内置逻辑包装为 InfoCollector 接口的 `.so`，验证已内置模块平滑插件化）
- [ ] `software-collector` — 软件盘点插件（全新实现：Linux dpkg/rpm，Windows 注册表，MacOS brew/pkgutil）
- [ ] `user-auditor` — 用户审计插件（全新实现：本地用户列表、用户组、当前登录）
- [ ] `process-monitor` — 进程监控插件（全新实现：进程列表采集 + 告警规则）
- [ ] `peripheral-scanner` — 外设扫描插件（全新实现：USB 设备列表 + 打印机）
- [ ] `disk-cleaner` — 磁盘清理插件（独立新增）

### 6.5 验证
- [ ] 编写一个测试插件，动态加载并执行
- [ ] 运行时替换插件文件，确认热重载生效
- [ ] 插件 panic 后，主进程和其他插件不受影响

---

## Phase 7 — 文件管理功能

> **目标**: 通过管理界面对远程终端进行文件浏览、上传、下载。

### 7.1 文件操作协议
- [ ] Protobuf 定义：`ListDirRequest/Response`、`UploadFile`、`DownloadFile`、`DeleteFile`、`RenameFile`
- [ ] 文件分片传输（大文件支持断点续传）

### 7.2 Client 端文件服务
- [ ] 目录列表（指定路径，返回文件/目录列表）
- [ ] 文件上传（管理界面 → Client）
- [ ] 文件下载（Client → 管理界面）
- [ ] 文件删除/重命名
- [ ] 路径安全校验（防止目录穿越攻击）

### 7.3 Server 端文件中转
- [ ] 文件分片中转（不落盘或临时缓存）
- [ ] 传输进度跟踪

### 7.4 管理界面文件管理页
- [ ] 文件浏览器组件（树形目录 + 文件列表）
- [ ] 上传/下载进度条
- [ ] 拖拽上传

### 7.5 文件内容搜索
- [ ] Client 端实现：支持按文件名模式（glob）和文件内容（grep）搜索
- [ ] 搜索结果回传
- [ ] 管理界面展示搜索结果

### 7.6 验证
- [ ] 浏览远程终端文件系统
- [ ] 上传/下载 100MB 文件，验证分片与断点续传
- [ ] 搜索指定目录下的 `.log` 文件内容，验证结果正确

---

## Phase 8 — 并发性能与压力测试

> **目标**: 验证 1000+ 终端并发连接，系统稳定运行。

### 8.1 并发优化
- [ ] Server 端 goroutine 池（避免无限制创建）
- [ ] 数据库连接池配置
- [ ] gRPC 连接参数调优（MaxConcurrentStreams、Keepalive）
- [ ] 内存与 CPU profiling（`pprof`）

### 8.2 模拟客户端
- [ ] 编写 `mock-client`（模拟 N 个终端的注册、心跳、上报行为）
- [ ] 支持配置：终端数量、心跳间隔、上报数据大小

### 8.3 压力测试
- [ ] 1000 终端同时连接，持续 30 分钟，记录：
  - Server 内存/CPU 占用
  - 消息延迟（P50/P99）
  - 连接成功率
  - 有无内存泄漏
- [ ] 2000 终端极限测试
- [ ] 网络抖动模拟（丢包率 5%、延迟 200ms），验证重连机制

### 8.4 测试报告
- [ ] 输出性能测试报告（图表 + 数据分析）

---

## Phase 9 — 在线升级

> **目标**: 客户端能接收并执行升级命令，自动更新自身。

### 9.1 升级流程
- [ ] Server 端：上传新版本客户端二进制 → 生成升级任务 → 下发升级命令
- [ ] Client 端：接收升级命令 → 下载新二进制 → 校验签名（SHA256）→ 替换自身 → 重启

### 9.2 安全措施
- [ ] 升级包签名校验（防止篡改）
- [ ] 版本回滚机制（保留旧版本备份）
- [ ] 升级失败自动回滚

### 9.3 验证
- [ ] 发布新版本 → 终端自动升级 → 确认版本号更新
- [ ] 模拟升级中断电 → 终端回滚到旧版本

---

## Phase 10 — 跨平台适配

> **目标**: 客户端在 Linux、Windows、MacOS 上均可编译运行。

### 10.1 平台适配层
- [ ] 抽象平台相关接口（信息采集、文件路径、进程管理）
- [ ] 平台条件编译（`//go:build linux` / `//go:build windows` / `//go:build darwin`）

### 10.2 平台特定实现
- [ ] Linux: 使用 `/proc`、`/sys`、`dpkg`/`rpm`、`systemd`
- [ ] Windows: 使用 WMI、注册表、Windows Service
- [ ] MacOS: 使用 `system_profiler`、`brew`、`launchd`

### 10.3 验证
- [ ] 在 Windows 上编译运行客户端
- [ ] 在 MacOS 上编译运行客户端
- [ ] 各平台基础采集数据一致

---

## Phase 11 — 安全加固

### 11.1 通信安全
- [ ] TLS/mTLS 加密通信
- [ ] 客户端证书认证

### 11.2 权限控制
- [ ] 管理界面用户认证（JWT）
- [ ] RBAC 角色权限（管理员、操作员、只读）

### 11.3 审计
- [ ] 操作日志记录（谁、何时、对哪个终端、执行了什么操作）
- [ ] 日志持久化与查询

---

## Phase 12 — 文档与交付

- [ ] API 文档（Protobuf 注释 + Swagger/OpenAPI）
- [ ] 部署文档（Docker Compose / systemd 部署方案）
- [ ] 用户手册（管理界面操作指南）
- [ ] 开发者文档（插件开发指南、架构设计说明）
- [ ] 项目答辩材料（PPT、演示视频）

---

## 开发顺序总结

```
Phase 0 → Phase 1 → Phase 2 → Phase 3 → Phase 4
                                              ↓
Phase 5 ←──────────────────────────────────────┘
   ↓
Phase 6 → Phase 7 → Phase 8 → Phase 9 → Phase 10 → Phase 11 → Phase 12
```

**关键里程碑**:
1. **M1 (Phase 1 完成)**: Client ↔ Server 互通，心跳正常 — *最小可用*
2. **M2 (Phase 3 完成)**: 信息采集 + 存储 + 查询 — *核心功能闭环*
3. **M3 (Phase 4 完成)**: Web 界面可看可操作 — *可演示*
4. **M4 (Phase 6 完成)**: 插件系统可用 — *扩展能力具备*
5. **M5 (Phase 8 完成)**: 1000 并发通过 — *性能达标*
