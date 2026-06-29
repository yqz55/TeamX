import { useEffect, useState, useCallback, type ReactNode } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Typography,
  Card,
  Tabs,
  Descriptions,
  Button,
  Space,
  Spin,
  Result,
  Tag,
  Table,
  Progress,
  Empty,
  Tooltip,
  message,
} from 'antd'
import {
  ArrowLeftOutlined,
  ReloadOutlined,
  ExperimentOutlined,
} from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { teamxClient } from '@/client'
import type {
  TerminalSummary,
  HardwareInfo,
  CPUInfo,
  MemoryInfo,
  DiskInfo,
  NetInfo,
  BIOSInfo,
  MotherboardInfo,
} from '@/gen/teamx_pb'

// =============================================================================
// helpers
// =============================================================================

/** Convert protoInt64 (bigint) to number, default 0. */
function int64(n: bigint | number | undefined): number {
  if (typeof n === 'bigint') return Number(n)
  if (typeof n === 'number') return n
  return 0
}

/** Format bytes to human-readable string. */
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let v = bytes
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** Mem usage ratio as 0–100 number. */
function memPercent(used: number, total: number): number {
  if (total <= 0) return 0
  return Math.round((used / total) * 100)
}

// =============================================================================
// plugin tab descriptor
// =============================================================================

export interface PluginTab {
  key: string
  label: string
  icon?: ReactNode
  /** True when Phase 6 provides a real data component. */
  ready: boolean
  /** Phase 6 replaces this with real content. */
  render: () => ReactNode
}

/** Standard placeholder for plugin tabs. Shows description of what the tab will contain. */
function PluginPlaceholder({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <Card>
      <Empty
        image={<ExperimentOutlined style={{ fontSize: 48, color: '#bfbfbf' }} />}
        description={
          <>
            <Typography.Text strong>{title}</Typography.Text>
            <br />
            <Typography.Text type="secondary">{description}</Typography.Text>
          </>
        }
      />
    </Card>
  )
}

// ---- registry (add new plugins here in Phase 6) ----------------------------

const pluginTabs: PluginTab[] = [
  {
    key: 'software',
    label: 'Software',
    ready: false,
    render: () => (
      <PluginPlaceholder
        title="Software Inventory"
        description="Installed software list and versions. Available in Phase 6."
      />
    ),
  },
  {
    key: 'users',
    label: 'Users',
    ready: false,
    render: () => (
      <PluginPlaceholder
        title="User Accounts"
        description="Local users, groups, and current logins. Available in Phase 6."
      />
    ),
  },
  {
    key: 'processes',
    label: 'Processes',
    ready: false,
    render: () => (
      <PluginPlaceholder
        title="Running Processes"
        description="Live process snapshot with CPU/memory usage. Available in Phase 6."
      />
    ),
  },
  {
    key: 'peripherals',
    label: 'Peripherals',
    ready: false,
    render: () => (
      <PluginPlaceholder
        title="Peripheral Devices"
        description="USB devices, printers, and external hardware. Available in Phase 6."
      />
    ),
  },
]

// =============================================================================
// hardware sub-components
// =============================================================================

function CpuSection({ cpu }: { cpu: CPUInfo }) {
  return (
    <Descriptions bordered column={{ xs: 1, sm: 2 }} size="small">
      <Descriptions.Item label="Model">{cpu.model || '-'}</Descriptions.Item>
      <Descriptions.Item label="Architecture">
        <Tag>{cpu.architecture || '-'}</Tag>
      </Descriptions.Item>
      <Descriptions.Item label="Cores">{cpu.cores || '-'}</Descriptions.Item>
      <Descriptions.Item label="Threads">{cpu.threads || '-'}</Descriptions.Item>
    </Descriptions>
  )
}

function MemorySection({ memory }: { memory: MemoryInfo }) {
  const total = int64(memory.totalBytes)
  const used = int64(memory.usedBytes)
  const pct = memPercent(used, total)
  return (
    <Descriptions bordered column={{ xs: 1, sm: 2 }} size="small">
      <Descriptions.Item label="Total">
        {formatBytes(total)}
      </Descriptions.Item>
      <Descriptions.Item label="Used">
        {formatBytes(used)}
      </Descriptions.Item>
      <Descriptions.Item label="Available">
        {formatBytes(int64(memory.availableBytes))}
      </Descriptions.Item>
      <Descriptions.Item label="Usage">
        <Progress
          percent={pct}
          size="small"
          status={pct > 90 ? 'exception' : pct > 70 ? 'normal' : 'success'}
          format={() => `${pct}%`}
        />
      </Descriptions.Item>
    </Descriptions>
  )
}

function DisksSection({ disks }: { disks: DiskInfo[] }) {
  const columns: ColumnsType<DiskInfo> = [
    { title: 'Device', dataIndex: 'device', key: 'device' },
    {
      title: 'Mount',
      dataIndex: 'mountPoint',
      key: 'mountPoint',
      render: (v: string) => v || '/',
    },
    { title: 'FS', dataIndex: 'fsType', key: 'fsType', width: 80 },
    {
      title: 'Total',
      key: 'total',
      render: (_: unknown, r: DiskInfo) => formatBytes(int64(r.totalBytes)),
    },
    {
      title: 'Used',
      key: 'used',
      render: (_: unknown, r: DiskInfo) => formatBytes(int64(r.usedBytes)),
    },
    {
      title: 'Free',
      key: 'free',
      render: (_: unknown, r: DiskInfo) => formatBytes(int64(r.freeBytes)),
    },
    {
      title: 'Usage',
      key: 'usage',
      render: (_: unknown, r: DiskInfo) => {
        const total = int64(r.totalBytes)
        const used = int64(r.usedBytes)
        const pct = total > 0 ? Math.round((used / total) * 100) : 0
        return (
          <Progress
            percent={pct}
            size="small"
            status={pct > 90 ? 'exception' : 'normal'}
          />
        )
      },
    },
  ]
  return (
    <Table
      columns={columns}
      dataSource={disks}
      rowKey="device"
      pagination={false}
      size="small"
      locale={{ emptyText: 'No disk data' }}
    />
  )
}

function NetsSection({ nets }: { nets: NetInfo[] }) {
  const columns: ColumnsType<NetInfo> = [
    { title: 'Name', dataIndex: 'name', key: 'name' },
    {
      title: 'MAC',
      dataIndex: 'macAddr',
      key: 'macAddr',
      render: (v: string) => <code>{v || '-'}</code>,
    },
    {
      title: 'IP Addresses',
      dataIndex: 'ipAddrs',
      key: 'ipAddrs',
      render: (v: string[]) =>
        v.length > 0 ? v.join(', ') : '-',
    },
    {
      title: 'Loopback',
      dataIndex: 'isLoopback',
      key: 'isLoopback',
      width: 90,
      render: (v: boolean) => (v ? <Tag>Yes</Tag> : '-'),
    },
  ]
  return (
    <Table
      columns={columns}
      dataSource={nets}
      rowKey="name"
      pagination={false}
      size="small"
      locale={{ emptyText: 'No network data' }}
    />
  )
}

function BiosSection({ bios }: { bios: BIOSInfo }) {
  return (
    <Descriptions bordered column={{ xs: 1, sm: 2 }} size="small">
      <Descriptions.Item label="Vendor">{bios.vendor || '-'}</Descriptions.Item>
      <Descriptions.Item label="Version">{bios.version || '-'}</Descriptions.Item>
      <Descriptions.Item label="Release Date">
        {bios.releaseDate || '-'}
      </Descriptions.Item>
    </Descriptions>
  )
}

function MotherboardSection({ mb }: { mb: MotherboardInfo }) {
  return (
    <Descriptions bordered column={{ xs: 1, sm: 2 }} size="small">
      <Descriptions.Item label="Manufacturer">
        {mb.manufacturer || '-'}
      </Descriptions.Item>
      <Descriptions.Item label="Product">{mb.product || '-'}</Descriptions.Item>
      <Descriptions.Item label="Serial">
        <Tooltip title={mb.serial || '-'}>
          <code>{mb.serial ? mb.serial.slice(0, 24) + '...' : '-'}</code>
        </Tooltip>
      </Descriptions.Item>
    </Descriptions>
  )
}

function HardwareTab({ hw }: { hw: HardwareInfo }) {
  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      {hw.cpu && (
        <Card title="CPU" size="small">
          <CpuSection cpu={hw.cpu} />
        </Card>
      )}
      {hw.memory && (
        <Card title="Memory" size="small">
          <MemorySection memory={hw.memory} />
        </Card>
      )}
      {hw.disks.length > 0 && (
        <Card title="Disks" size="small">
          <DisksSection disks={hw.disks} />
        </Card>
      )}
      {hw.nets.length > 0 && (
        <Card title="Network" size="small">
          <NetsSection nets={hw.nets} />
        </Card>
      )}
      {hw.bios && (
        <Card title="BIOS" size="small">
          <BiosSection bios={hw.bios} />
        </Card>
      )}
      {hw.motherboard && (
        <Card title="Motherboard" size="small">
          <MotherboardSection mb={hw.motherboard} />
        </Card>
      )}
      {!hw.cpu && !hw.memory && hw.disks.length === 0 && hw.nets.length === 0 && (
        <Empty description="No hardware report received yet." />
      )}
    </Space>
  )
}

// =============================================================================
// main component
// =============================================================================

export default function TerminalDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const [summary, setSummary] = useState<TerminalSummary | null>(null)
  const [hardware, setHardware] = useState<HardwareInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    if (!id) return
    setLoading(true)
    setError(null)
    try {
      const resp = await teamxClient.getTerminal({ sessionId: id })
      setSummary(resp.summary ?? null)
      setHardware(resp.latestHardware ?? null)
    } catch (e) {
      setError(String(e))
      message.error(`Failed to load terminal: ${String(e)}`)
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  // ---- loading / error -----------------------------------------------------
  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size="large" />
        <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
          Loading terminal {id}...
        </Typography.Paragraph>
      </div>
    )
  }

  if (error) {
    return (
      <Result
        status="error"
        title="Failed to Load Terminal"
        subTitle={error}
        extra={
          <Button
            type="primary"
            icon={<ReloadOutlined />}
            onClick={fetchData}
          >
            Retry
          </Button>
        }
      />
    )
  }

  if (!summary) {
    return (
      <Result
        status="404"
        title="Terminal Not Found"
        subTitle={`No terminal found for "${id}". It may have disconnected.`}
        extra={
          <Button type="primary" onClick={() => navigate('/')}>
            Back to List
          </Button>
        }
      />
    )
  }

  // ---- data loaded ---------------------------------------------------------

  /** Build the tab list: built-in Overview + Hardware, then plugin tabs. */
  const tabItems = [
    {
      key: 'overview',
      label: 'Overview',
      children: (
        <Card>
          <Descriptions bordered column={{ xs: 1, sm: 2 }} size="small">
            <Descriptions.Item label="Session ID">
              <Tooltip title={summary.sessionId}>
                <code>{summary.sessionId.slice(0, 16)}...</code>
              </Tooltip>
            </Descriptions.Item>
            <Descriptions.Item label="Device ID">
              <Tooltip title={summary.deviceId}>
                <code>{summary.deviceId.slice(0, 16)}...</code>
              </Tooltip>
            </Descriptions.Item>
            <Descriptions.Item label="Hostname">
              {summary.hostname}
            </Descriptions.Item>
            <Descriptions.Item label="Status">
              <Tag color={summary.online ? 'green' : 'red'}>
                {summary.online ? 'Online' : 'Offline'}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="OS">
              {summary.os} {summary.osVersion}
            </Descriptions.Item>
            <Descriptions.Item label="Client Version">
              {summary.clientVersion || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Last Heartbeat">
              {summary.lastHeartbeat || 'Never'}
            </Descriptions.Item>
            <Descriptions.Item label="Last Seen">
              {summary.lastSeenAt || '-'}
            </Descriptions.Item>
          </Descriptions>
        </Card>
      ),
    },
    {
      key: 'hardware',
      label: 'Hardware',
      children: hardware ? (
        <HardwareTab hw={hardware} />
      ) : (
        <Card>
          <Empty description="No hardware report received yet. The client will report hardware info after connecting." />
        </Card>
      ),
    },
    // Append plugin tabs (Software, Users, Processes, Peripherals).
    ...pluginTabs.map((p) => ({
      key: p.key,
      label: p.label,
      children: p.render(),
    })),
  ]

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/')}>
          Back
        </Button>
        <Button icon={<ReloadOutlined />} onClick={fetchData}>
          Refresh
        </Button>
      </Space>

      <Typography.Title level={3}>{summary.hostname}</Typography.Title>

      <Tabs defaultActiveKey="overview" items={tabItems} />
    </div>
  )
}
