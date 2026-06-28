# TeamX Protocol Specification v0.2

## Overview

TeamX uses **gRPC** as its communication framework with **Protobuf** as the IDL. The protocol defines three RPCs covering registration, real-time bidirectional communication, and file transfer.

| RPC | Type | Purpose | Phase |
|---|---|---|---|
| `Register` | Unary | One-shot handshake: client registration & authentication | 0.2 |
| `Channel` | Bidirectional Stream | Heartbeat, info reporting, command dispatch, command results | 0.2 |
| `TransferFile` | Bidirectional Stream | Chunked file upload / download with resume support | 7 |

---

## 1. Register (Unary)

Client sends its hardware-backed device fingerprint (`device_id`) to the server;
the server assigns a unique `session_id` for this connection and checks the blocklist.

### Flow

```
Client                            Server
   |                                 |
   |--- HandshakeRequest ----------->|
   |     (device_id + hostname)      |
   |                                 |
   |<-- HandshakeResponse -----------|
   |                                 |
   | (use session_id for all         |
   |  subsequent Channel calls)      |
```

### HandshakeRequest

| Field | Type | Description |
|---|---|---|
| `hostname` | string | Hostname of the client machine |
| `os` | string | OS: `linux`, `windows`, `darwin` |
| `os_version` | string | Human-readable OS version |
| `kernel_version` | string | Kernel / build version |
| `client_version` | string | TeamX client semver (e.g. `0.2.0`) |
| `mac_addrs` | []string | All MAC addresses |
| `ip_addrs` | []string | All non-loopback IP addresses |
| `device_id` | string | Hardware fingerprint (SHA-256 of DMI UUID + MAC + disk serial + machine-id) |

### HandshakeResponse

| Field | Type | Description |
|---|---|---|
| `ok` | bool | `true` = registered; `false` = denied (device blocked) |
| `session_id` | string | Server-assigned session UUID v4 |
| `server_time` | string | Server clock (RFC 3339), for time sync |
| `message` | string | Welcome message or denial reason |

---

## 2. Channel (Bidirectional Stream)

The main communication pipe. After `Register`, the client opens a single long-lived `Channel` stream. Both sides send frames independently. The client attaches its `session_id` via gRPC metadata header `session-id`.

### Stream Messages

```
ClientMessage (client → server)
├── heartbeat          Heartbeat           每 10s 发送
├── report_request     ReportRequest       信息上报（硬件/软件/用户/进程/外设）
└── command_result     CommandResult       命令执行结果回传

ServerMessage (server → client)
├── heartbeat_ack      HeartbeatAck        心跳确认
└── command            Command             命令下发
```

Both `ClientMessage` and `ServerMessage` carry a `uint64 seq` for deduplication and out-of-order detection.

---

## 3. Message Details

### 3.1 Heartbeat

Sent by the client every **10 seconds**.

| Field | Type | Description |
|---|---|---|
| `timestamp_unix` | int64 | Client wall-clock (unix seconds) |
| `cpu_percent` | float | Overall CPU utilization % |
| `mem_percent` | float | Overall memory utilization % |
| `goroutines` | int32 | Client process goroutine count |

### 3.2 HeartbeatAck

Sent by the server in response to each Heartbeat.

| Field | Type | Description |
|---|---|---|
| `server_time_unix` | int64 | Server wall-clock at receive time |

**Timeout rule**: If the server hasn't received a Heartbeat for **30 seconds**, the client is marked **offline**.

---

### 3.3 ReportRequest

Used for uploading system information to the server. Each report has a unique `report_id` (UUID v4) for deduplication. Large reports (SoftwareInfo, ProcessInfo) may be split across multiple frames using `total_count`.

| Field | Type | Description |
|---|---|---|
| `report_id` | string | Unique report identifier |
| `type` | oneof | One of: `hardware`, `software`, `user`, `process`, `peripheral` |

#### 3.3.1 HardwareInfo

Captures the client machine's hardware profile.

| Sub-message | Key Fields |
|---|---|
| `CPUInfo` | model, cores, threads, architecture |
| `MemoryInfo` | total_bytes, available_bytes, used_bytes |
| `DiskInfo` | device, mount_point, fs_type, total/used/free |
| `NetInfo` | name, mac_addr, ip_addrs, is_loopback |
| `BIOSInfo` | vendor, version, release_date |
| `MotherboardInfo` | manufacturer, product, serial |

#### 3.3.2 SoftwareInfo

Installed software inventory. May be paged via `total_count`.

| Sub-message | Key Fields |
|---|---|
| `SoftwareItem` | name, version, publisher, install_date |

#### 3.3.3 UserInfo

Local user accounts and currently logged-in users.

| Sub-message | Key Fields |
|---|---|
| `UserAccount` | username, uid, group, home_dir, shell, is_admin, is_disabled |

#### 3.3.4 ProcessInfo

Running process snapshot. May be paged.

| Sub-message | Key Fields |
|---|---|
| `ProcessItem` | pid, ppid, name, user, cpu_percent, mem_bytes, status, cmdline |

#### 3.3.5 PeripheralInfo

Connected external devices.

| Sub-message | Key Fields |
|---|---|
| `USBDevice` | name, vendor_id, product_id, serial |
| `Printer` | name, driver, port, is_default |

---

### 3.4 Command

Sent by the server to instruct a client to perform an action.

| Field | Type | Description |
|---|---|---|
| `command_id` | string | Unique command UUID v4 |
| `type` | string | Command type (see below) |
| `params` | map<string,string> | Type-specific parameters |
| `timeout_sec` | int64 | Execution timeout (0 = no limit) |
| `created_at_unix` | int64 | Command creation timestamp |

**Command Types**:

| Type | Description | Params |
|---|---|---|
| `CollectNow` | Trigger immediate full info collection | (none) |
| `RunScript` | Execute a shell script | `script`, `interpreter` (default: `/bin/sh`) |
| `KillProcess` | Terminate a process | `pid` |
| `UpdateConfig` | Update client configuration | `key=value` pairs |
| `Upgrade` | Download and apply new client version | `url`, `checksum_sha256` |
| `Restart` | Restart the client process | (none) |
| `Shutdown` | Gracefully shut down the client | (none) |

### 3.5 CommandResult

Client sends this back after executing (or failing to execute) a command.

| Field | Type | Description |
|---|---|---|
| `command_id` | string | Matches the originating Command |
| `status` | string | `Executing` / `Completed` / `Failed` / `Timeout` |
| `exit_code` | int32 | Process exit code |
| `stdout` | string | Captured standard output |
| `stderr` | string | Captured standard error |
| `started_at_unix` | int64 | Execution start time |
| `finished_at_unix` | int64 | Execution end time |
| `error_message` | string | Error description if Failed |

**Status lifecycle**:
```
Pending → Sent → Executing → Completed
                            → Failed
                            → Timeout
```
- `Executing` is sent immediately when the client starts the command (acknowledgement).
- `Completed`, `Failed`, or `Timeout` is the final state.
- If no final response arrives within `timeout_sec`, the server auto-marks `Timeout`.

---

## 4. File Transfer (Phase 7, prototyped in 0.2)

File transfer uses a separate bidirectional stream for chunked data.

### Flow (Upload: Admin → Server → Client)

```
Sender                            Receiver
   |                                  |
   |--- FileTransferRequest --------->|
   |<-- FileTransferResponse --------|
   |                                  |
   |--- FileData (chunk 0) ---------->|
   |--- FileData (chunk 1) ---------->|
   | ...                              |
   |--- FileData (last, is_last) ---->|
```

### Messages

| Message | Key Fields |
|---|---|
| `FileTransferRequest` | transfer_id, path, size_bytes, direction, chunk_size |
| `FileTransferResponse` | transfer_id, accepted, message, resume_offset |
| `FileData` | transfer_id, offset, size, data, is_last, checksum |

### Resume

If the receiver responds with `resume_offset > 0`, the sender continues from that byte offset. Only works when `transfer_id` matches a previously interrupted transfer.

---

## 5. Error Codes

Errors are transmitted via **gRPC status metadata** (`grpc-status-details-bin`).

| Code | Name | Description |
|---|---|---|
| 0 | `OK` | Success |
| **1000** | `ERR_HANDSHAKE_DENIED` | Device denied by blocklist |
| **1001** | `ERR_SESSION_NOT_FOUND` | Specified `session_id` does not exist |
| **1002** | `ERR_SESSION_OFFLINE` | Target session is offline |
| **2000** | `ERR_COMMAND_TIMEOUT` | Command execution exceeded timeout |
| **2001** | `ERR_COMMAND_UNKNOWN` | Unknown command type string |
| **3000** | `ERR_FILE_NOT_FOUND` | Requested file path does not exist |
| **3001** | `ERR_FILE_PERMISSION` | Insufficient file permissions |
| **3002** | `ERR_PATH_TRAVERSAL` | Path traversal attack detected |
| **4000** | `ERR_TRANSFER_MISMATCH` | `transfer_id` mismatch |
| **4001** | `ERR_TRANSFER_CHECKSUM` | Chunk checksum verification failed |
| **5000** | `ERR_INTERNAL` | Internal server error |

Error code ranges:
- `0` — Success
- `1xxx` — Registration / authentication
- `2xxx` — Command execution
- `3xxx` — File system
- `4xxx` — Transfer protocol
- `5xxx` — Internal

---

## 6. Concurrency & Ordering

- **seq field**: Every `ClientMessage` and `ServerMessage` carries a monotonically increasing `seq`. Receivers use this to detect gaps (lost messages) and duplicates.
- **Heartbeat**: Periodic, independent of command/report traffic.
- **Commands**: Processed in FIFO order per client. A client must finish (or timeout) command N before starting N+1.
- **Reports**: Fire-and-forget. No response required from the server beyond the TCP/HTTP2 ACK.

---

## 7. Future Considerations

- **TLS / mTLS** (Phase 11): Wrap gRPC with mutual TLS for encryption and client certificate authentication.
- **Compression**: Enable gRPC compression (gzip/snappy) for large ReportRequests.
- **Backpressure**: If the server cannot keep up, it may close the stream with `RESOURCE_EXHAUSTED`. The client should reconnect with exponential backoff.
