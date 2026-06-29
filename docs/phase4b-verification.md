# Phase 4b — 前端管理界面验证手册

> 验证目标：浏览器端 SPA（React + TypeScript + Vite + Ant Design），通过 ConnectRPC 调用
> Phase 4a 的 HTTP 网关，展示终端列表、详情，支持踢断/封禁/解封操作，WebSocket 实时更新。
>
> **Phase 4b 分三个子阶段**：
> - 4b.1 — 项目搭建（完成 ✅）
> - 4b.2 — 终端列表页（完成 ✅）
> - 4b.3 — 终端详情页 + 插件 Tab 架构（完成 ✅）

---

## 1. 架构概览

```
Browser                                 Admin Gateway (:8080)          TeamX Server (:50051)
═══════                                 ═══════════════════════          ════════════════════

React SPA (Vite :5173)
  │
  ├─ GET  /                              TerminalList page
  │    ├─ POST ListTerminals ──────────► Connect handler ──gRPC──► ListTerminals RPC
  │    ├─ POST DisconnectTerminal ─────► Connect handler ──gRPC──► DisconnectTerminal RPC
  │    ├─ POST BlockTerminal ──────────► Connect handler ──gRPC──► BlockTerminal RPC
  │    ├─ POST UnblockTerminal ────────► Connect handler ──gRPC──► UnblockTerminal RPC
  │    └─ WS  /ws ═════════════════════► wsHub (poll 5s)           (listens :50051)
  │         online/offline push ◄────────┘
  │
  └─ GET  /terminal/:id                 TerminalDetail page
       └─ POST GetTerminal ────────────► Connect handler ──gRPC──► GetTerminal RPC
```

### 1.1 技术栈

| 层 | 技术 |
|---|---|
| 框架 | React 19 + TypeScript 5.8 |
| 构建 | Vite 6 |
| 组件库 | Ant Design 5 + @ant-design/icons |
| 路由 | React Router 7 (`createBrowserRouter`) |
| RPC | @connectrpc/connect v1 + @connectrpc/connect-web v1 |
| Proto 代码生成 | buf + protoc-gen-es v1 + protoc-gen-connect-es v1 |
| WebSocket | 原生 `WebSocket` API（`useWebSocket` hook） |

### 1.2 新增/修改文件

| 文件 | 说明 |
|---|---|
| `web/package.json` | React + Vite + Ant Design + ConnectRPC 依赖 |
| `web/buf.gen.yaml` | TypeScript proto 代码生成（独立于根 Go 配置） |
| `web/tsconfig.json` | TypeScript 项目引用根配置 |
| `web/tsconfig.app.json` | 应用 TS 配置（strict, `@/*` 路径别名） |
| `web/tsconfig.node.json` | Vite 配置专用 TS 配置 |
| `web/vite.config.ts` | Vite 6 + React 插件 + `@/` 别名 |
| `web/index.html` | SPA 入口（`<html lang="zh-CN">`） |
| `web/.gitignore` | `node_modules/`, `dist/`, `.vite/` |
| `web/src/vite-env.d.ts` | Vite 类型声明 |
| `web/src/main.tsx` | React 入口 + Ant Design `ConfigProvider`（zh_CN locale）+ `<App>` 包装 |
| `web/src/App.tsx` | `RouterProvider` 挂载 |
| `web/src/App.css` | 全局样式 + terminal-list 布局类 |
| `web/src/router.tsx` | 路由: `/` → TerminalList, `/terminal/:id` → TerminalDetail |
| `web/src/client.ts` | ConnectRPC 单例客户端（`createClient` + `createConnectTransport`） |
| `web/src/ws.ts` | `useWebSocket` hook（类型安全事件 + ref 回调避免重连） |
| `web/src/components/AppLayout.tsx` | Ant Design `Layout`（可折叠侧栏 + 顶栏 + `<Outlet />`） |
| `web/src/pages/TerminalList.tsx` | 终端列表页（4b.2） |
| `web/src/pages/TerminalDetail.tsx` | 终端详情页 + PluginTab 架构（4b.3） |
| `web/src/gen/teamx_pb.ts` | buf 生成 — Proto 消息类型 |
| `web/src/gen/teamx_connect.ts` | buf 生成 — Connect 服务描述符 |
| `.gitignore` | 新增 `web/node_modules/`, `web/dist/`, `web/src/gen/` |

---

## 2. 页面功能一览

### 2.1 TerminalList (`/`)

| 功能 | 实现方式 |
|---|---|
| **数据获取** | `useEffect([filter, page, pageSize])` → `teamxClient.listTerminals()` |
| **表格列** | Session ID（截断 + Tooltip）、Hostname（可排序）、OS + 版本、Client Ver、Status（Tag）、Last Heartbeat（相对时间 + 可排序）、Actions |
| **过滤器** | `Radio.Group`: All / Online / Offline → 服务端 `onlineFilter` 参数；切换 reset page=1 |
| **分页** | Ant Design `Table` 内置 pagination，10/20/50 可选，`showTotal` |
| **Detail** | `navigate(\`/terminal/${sessionId}\`)` |
| **Kick** | `Modal.confirm` → `disconnectTerminal({sessionId})` → 刷新表格 |
| **Block** | `Modal.confirm` → `blockTerminal({deviceId})` → 刷新表格 |
| **Unblock** | `Modal.confirm` → `unblockTerminal({deviceId})` → 刷新表格 |
| **WebSocket** | `useWebSocket({onEvent})` — 已知 session 本地更新 online 状态；未知 session 则 refetch |
| **WS 状态指示** | 工具栏 `Badge` 显示 "Live" / "WS off" |
| **Refresh 按钮** | 手动重新请求当前页数据 |
| **空状态** | "No terminals connected. Start a client to see data here." |
| **错误处理** | `message.error()` 弹出错误提示 |

### 2.2 TerminalDetail (`/terminal/:id`)

| Tab | 内容 | 数据来源 |
|---|---|---|
| **Overview** | `Descriptions`: Session ID、Device ID、Hostname、OS/版本、Client Ver、Status（Tag）、Last Heartbeat、Last Seen | `response.summary` |
| **Hardware** | CPU（`Descriptions`）、Memory（`Progress` 百分比）、Disks（`Table` + `Progress`）、Network（`Table`）、BIOS（`Descriptions`）、Motherboard（`Descriptions`） | `response.latestHardware` |
| **Software** | 占位 `Empty` — "Available in Phase 6" | — |
| **Users** | 占位 `Empty` — "Available in Phase 6" | — |
| **Processes** | 占位 `Empty` — "Available in Phase 6" | — |
| **Peripherals** | 占位 `Empty` — "Available in Phase 6" | — |

**插件 Tab 架构**：

```typescript
export interface PluginTab {
  key: string          // Tab key
  label: string        // Tab 标题
  icon?: ReactNode     // Tab 图标
  ready: boolean       // Phase 6 改为 true
  render: () => ReactNode  // Phase 6 替换为真实组件
}

// 注册新插件只需在 pluginTabs 数组中添加一项
const pluginTabs: PluginTab[] = [
  { key: 'software',    label: 'Software',    ready: false, render: () => <Placeholder /> },
  { key: 'users',       label: 'Users',       ready: false, render: () => <Placeholder /> },
  { key: 'processes',   label: 'Processes',   ready: false, render: () => <Placeholder /> },
  { key: 'peripherals', label: 'Peripherals', ready: false, render: () => <Placeholder /> },
]
```

**状态处理**：

| 状态 | 组件 |
|---|---|
| Loading | 居中 `Spin` + "Loading terminal {id}..." |
| Error | `Result` (error) + Retry 按钮 |
| Not Found | `Result` (404) + Back to List 按钮 |
| 无硬件数据 | `Empty` — "No hardware report received yet." |

---

## 3. 编译与启动

### 3.1 前置条件

- Node.js ≥ 18（当前 v24.13.1）
- npm registry 已切中国镜像：`npm config set registry https://registry.npmmirror.com`
- Phase 4a 的 Server + Gateway 已启动

### 3.2 安装依赖

```powershell
cd D:\MyProjects\TeamX\web
npm install
```

### 3.3 生成 TypeScript Proto 代码

```powershell
npm run generate
```

输出 `web/src/gen/teamx_pb.ts` + `web/src/gen/teamx_connect.ts`。

> 修改 `internal/proto/teamx.proto` 后需重新运行此命令。

### 3.4 开发模式

```powershell
npm run dev
```

Vite dev server → `http://localhost:5173`。

### 3.5 生产构建

```powershell
npm run build    # tsc -b && vite build → dist/
npm run preview  # 预览构建产物
```

---

## 4. 页面验证

> 需要先启动 Server + Gateway：
> ```powershell
> # 终端 A — Server
> .\bin\server.exe --port 50051
>
> # 终端 B — Gateway
> .\bin\admin.exe serve --http-port 8080 --poll-interval 5
> ```

### 4.1 项目启动 — 空白状态

1. 启动 Vite dev server：`cd web && npm run dev`
2. 浏览器访问 `http://localhost:5173`

**预期**：
- 左侧深色侧栏（"TeamX" 标题 + "Terminals" 菜单项，可折叠）
- 右侧白色顶栏（"TeamX Admin" 标题）
- 内容区显示 "Terminal List" 标题 + 工具栏（All/Online/Offline 过滤器 + WS 状态 + Refresh 按钮）
- 表格显示空状态："No terminals connected. Start a client to see data here."

### 4.2 终端列表 — 有数据

1. 启动一个或多个客户端连接 Server
2. 刷新页面或等待自动加载

**预期**：
- 表格显示终端行，每行包含：
  - **Session ID** — 截断显示前 12 字符 + `...`，鼠标悬浮 Tooltip 显示完整
  - **Hostname** — 可点击列头排序
  - **OS** — "linux" / "windows" + 版本号
  - **Client Ver** — "0.2.0" 或 "-"
  - **Status** — 绿色 `Online` Tag / 红色 `Offline` Tag
  - **Last Heartbeat** — 相对时间 "just now" / "3m ago" / "2d ago"，悬浮显示完整时间戳，默认倒序
  - **Actions** — Detail（始终可见）+ Kick（仅在线）+ Block + Unblock

### 4.3 在线/离线过滤器

1. 点击 "Online" filter
2. 点击 "Offline" filter
3. 点击 "All"

**预期**：
- "Online" — 只显示在线终端
- "Offline" — 只显示离线终端
- "All" — 显示所有
- 切换 filter 后页码重置为 1

### 4.4 分页

1. 启动足够多的客户端（超过 10/20 个）或在 page_size=10 时验证

**预期**：
- 底部 pagination 显示页码、每页条数选择器（10/20/50）、总数汇总（"N terminals"）
- 切换页码和每页条数正确加载数据

### 4.5 排序

1. 点击 "Hostname" 列头 — 字母排序切换（升序/降序/取消）
2. 点击 "Last Heartbeat" 列头 — 时间排序切换（默认降序）

**预期**：表格数据正确排序。

### 4.6 Detail 跳转

1. 点击某终端行的 "Detail" 按钮

**预期**：
- 跳转到 `/terminal/<session-id>`
- 页面显示 Back 按钮 + Refresh 按钮
- 标题为终端 hostname
- 默认选中 "Overview" tab

### 4.7 Kick 操作

1. 回到列表页
2. 对一个在线终端点击 "Kick"
3. 确认 Modal 弹窗内容（显示 hostname + 截断 session_id）
4. 点击 "Kick" 按钮

**预期**：
- `message.success("Kicked {hostname}")` 弹出
- 表格自动刷新，该终端变为 Offline 或消失
- 被踢客户端进程退出

### 4.8 Block / Unblock 操作

1. 点击某终端的 "Block"
2. 确认 Modal 弹窗内容（显示截断 device_id）
3. 点击 "Block"
4. 客户端重启验证无法注册
5. 点击 "Unblock"
6. 客户端重启验证可再次注册

**预期**：
- Block → `message.success("Blocked {hostname}")`，所有会话被踢
- Unblock → `message.success("Unblocked {hostname}")`
- 表格自动刷新

### 4.9 WebSocket 实时更新

1. 打开列表页，确认 WS 指示灯显示绿色 "Live"
2. 启动一个新客户端 → 列表自动出现新行（或 refetch）
3. Ctrl+C 关闭客户端 → 对应行 Status 保持 Offline（本地更新不 refetch）

**预期**：
- 网关 WebSocket 推送后，已存在于当前页的终端其在线状态实时变化
- WS 断开时指示灯显示灰色 "WS off"

### 4.10 终端详情 — Overview Tab

1. 点击某个在线终端的 "Detail"

**预期**：
- `Descriptions` 组件展示 8 个字段：
  - **Session ID** — 截断 + Tooltip
  - **Device ID** — 截断 + Tooltip
  - **Hostname** — 完整主机名
  - **Status** — 绿色/红色 Tag
  - **OS** — 操作系统 + 版本
  - **Client Version** — 客户端版本号
  - **Last Heartbeat** — RFC3339 时间或 "Never"
  - **Last Seen** — RFC3339 时间或 "-"

### 4.11 终端详情 — Hardware Tab

1. 客户端已上报硬件数据（Linux VM 完整上报，Windows 可能空）
2. 切换到 "Hardware" tab

**预期**（有数据时）：
- **CPU** — 小卡片：Model、Architecture（Tag）、Cores、Threads
- **Memory** — 小卡片：Total、Used、Available（格式化单位） + 使用率 `Progress` 条
- **Disks** — 小卡片：`Table` 含 Device、Mount、FS、Total、Used、Free、Usage（`Progress` 条）
- **Network** — 小卡片：`Table` 含 Name、MAC、IP Addresses、Loopback（Tag）
- **BIOS** — 小卡片（如有）：Vendor、Version、Release Date
- **Motherboard** — 小卡片（如有）：Manufacturer、Product、Serial

**预期**（无数据时）：
- `Empty` 组件："No hardware report received yet."

### 4.12 终端详情 — 插件占位 Tab

1. 切换到 Software / Users / Processes / Peripherals 各 tab

**预期**：
- 每个 tab 显示统一风格的 `Empty` 卡片：
  - 实验瓶图标
  - 插件名称（Software Inventory / User Accounts / Running Processes / Peripheral Devices）
  - "Available in Phase 6" 描述文字

### 4.13 详情页 — Refresh 按钮

1. 在详情页点击 "Refresh"

**预期**：重新请求 `getTerminal`，数据刷新。

### 4.14 错误处理

**Server 未启动时**：
- 列表页：`message.error("Failed to load terminals: ...")`
- 详情页：`Result` (error) + Retry 按钮

**终端不存在时**：
- 访问 `/terminal/nonexistent-id` → `Result` (404) + "Back to List" 按钮

---

## 5. ConnectRPC 客户端验证

在浏览器 DevTools Console 中验证客户端正常工作：

```js
// 导入路径在模块中可用，console 中直接测 HTTP:
// 验证 ListTerminals
fetch('http://localhost:8080/teamx.proto.TeamX/ListTerminals', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ page: 1, page_size: 10 })
}).then(r => r.json()).then(console.log)
// → { terminals: [...], totalCount: N }

// 验证 GetTerminal
fetch('http://localhost:8080/teamx.proto.TeamX/GetTerminal', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ session_id: '<session-id>' })
}).then(r => r.json()).then(console.log)
// → { summary: {...}, latestHardware: {...} | null }

// 验证 CORS
fetch('http://localhost:5173')  // Vite dev server
// 从 localhost:5173 发请求到 localhost:8080，应成功跨域
```

---

## 6. WebSocket 验证

在浏览器 DevTools Console：

```js
const ws = new WebSocket('ws://localhost:8080/ws')
ws.onmessage = e => console.log('WS event:', JSON.parse(e.data))
ws.onopen  = () => console.log('WS connected')
ws.onclose = () => console.log('WS closed')
```

**预期**：
- 连接后 `Console` 显示 "WS connected"
- 客户端上线/下线时收到 JSON 事件：`{type, session_id, hostname, timestamp}`
- 页面工具栏 WS 指示灯实时变化

---

## 7. 通过标准

| # | 验证项 | 预期 | 状态 |
|---|--------|------|------|
| 1 | `npm install` | 无错误，149 个包安装完成 | ☐ |
| 2 | `npm run generate` | `src/gen/teamx_pb.ts` + `src/gen/teamx_connect.ts` 生成 | ☐ |
| 3 | `npx tsc -b` | 零 TypeScript 类型错误 | ☐ |
| 4 | `npm run build` | `dist/` 生成，构建成功 | ☐ |
| 5 | `npm run dev` | Vite dev server 启动于 `:5173` | ☐ |
| 6 | 空白列表页 | AppLayout（侧栏 + 顶栏）正确渲染；空状态文字显示 | ☐ |
| 7 | 有数据列表 | 表格显示所有列（Session ID / Hostname / OS / Client Ver / Status / Last Heartbeat / Actions） | ☐ |
| 8 | 在线/离线过滤 | Radio.Group 切换 All/Online/Offline，服务端过滤，页码重置 | ☐ |
| 9 | 分页 | 分页控件正常；切换每页条数（10/20/50）和页码正确加载 | ☐ |
| 10 | 列排序 | Hostname 排序、Last Heartbeat 默认降序 | ☐ |
| 11 | Session ID Tooltip | 截断显示 12 字符 + "..."，悬浮显示完整 ID | ☐ |
| 12 | Detail 跳转 | 点击 Detail → `/terminal/<sessionId>` | ☐ |
| 13 | Kick 操作 | Modal 确认 → `message.success` → 表格刷新 → 客户端退出 | ☐ |
| 14 | Block 操作 | Modal 确认 → `message.success` → 表格刷新 → 客户端无法重连 | ☐ |
| 15 | Unblock 操作 | Modal 确认 → `message.success` → 客户端可重连 | ☐ |
| 16 | WebSocket 实时更新 | WS 指示灯绿色；客户端上线/离线后表格对应行 Status 即时更新 | ☐ |
| 17 | Refresh 按钮 | 手动刷新列表数据（loading 态显示） | ☐ |
| 18 | Overview Tab | 8 字段完整展示（session_id / device_id / hostname / status / os / client_ver / heartbeat / seen） | ☐ |
| 19 | Hardware Tab — 有数据 | CPU/Memory/Disks/Network/BIOS/Motherboard 各 section 正确渲染 | ☐ |
| 20 | Hardware Tab — 无数据 | `Empty` 组件 + "No hardware report received yet." | ☐ |
| 21 | Memory/Disk Progress 条 | 用量百分比正确，颜色区分（绿色正常 / 红色 > 90%） | ☐ |
| 22 | Software Tab | 占位 `Empty` — "Available in Phase 6" | ☐ |
| 23 | Users Tab | 占位 `Empty` — "Available in Phase 6" | ☐ |
| 24 | Processes Tab | 占位 `Empty` — "Available in Phase 6" | ☐ |
| 25 | Peripherals Tab | 占位 `Empty` — "Available in Phase 6" | ☐ |
| 26 | 详情页 Loading | 居中 `Spin` + "Loading terminal {id}..." | ☐ |
| 27 | 详情页 Error | `Result` (error) + Retry 按钮 | ☐ |
| 28 | 详情页 404 | `Result` (404) + "Back to List" 按钮 | ☐ |
| 29 | 详情页 Refresh | 重新请求 getTerminal，数据刷新 | ☐ |
| 30 | 详情页 Back 按钮 | 导航回 `/`（列表页） | ☐ |
| 31 | PluginTab export | `TerminalDetail.tsx` 导出 `PluginTab` 接口，Phase 6 可直接引用 | ☐ |
| 32 | CORS / Connect 正常 | 从 `:5173` 前端调用 `:8080` 网关无跨域错误 | ☐ |
| 33 | Router URL | `/` 和 `/terminal/:id` 两种 URL 浏览器直接输入均正常渲染 | ☐ |

全部勾选 = **Phase 4b 完成** 🎉

---

## 8. 已知限制

| 限制 | 说明 | 计划 |
|---|---|---|
| 无数据缓存 | 每次切换页面/筛选器都重新请求（含 WebSocket 触发的未知 session refetch） | 可加 React Query / SWR 缓存 |
| 大 chunk 警告 | JS bundle ~1.2MB（Ant Design + ConnectRPC），超过 500KB 建议 | 后续可分 chunk 加载 |
| ConnectRPC v1 | 代码生成插件 `protoc-gen-connect-es v1.7` 依赖 protobuf-es v1 | 等待 connect-es codegen v2 发布后升级 |
| 无命令下发 UI | 列表页/详情页无命令发送功能 | Phase 5 可新增 "Commands" 页面或终端行添加 "Send Command" 按钮 |
| 无文件管理 UI | 无文件浏览/上传/下载界面 | Phase 7 |
| 无认证 UI | 无登录页面或 token 管理 | Phase 11 |
| 插件 Tab 无数据 | Software/Users/Processes/Peripherals 为占位 | Phase 6 插件系统 |
| WebSocket WS 状态轮询 | 指示灯通过 hook `isConnected` 更新，非实时心跳 | 可改用健康检查端点 |
