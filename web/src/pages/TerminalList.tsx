import { useEffect, useState, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Typography,
  Table,
  Card,
  Space,
  Tag,
  Radio,
  Button,
  Modal,
  Tooltip,
  message,
  Badge,
} from 'antd'
import type { ColumnsType, TablePaginationConfig } from 'antd/es/table'
import {
  ReloadOutlined,
  EyeOutlined,
  StopOutlined,
  LockOutlined,
  UnlockOutlined,
} from '@ant-design/icons'
import { teamxClient } from '@/client'
import { useWebSocket, type WsEvent } from '@/ws'
import { TerminalSummary } from '@/gen/teamx_pb'

// ---- helpers ---------------------------------------------------------------

function shortId(id: string, n = 12): string {
  if (id.length <= n) return id
  return id.slice(0, n) + '...'
}

function timeAgo(iso: string): string {
  if (!iso) return '-'
  const diff = Date.now() - new Date(iso).getTime()
  const sec = Math.floor(diff / 1000)
  if (sec < 60) return 'just now'
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h ago`
  const days = Math.floor(hr / 24)
  if (days < 30) return `${days}d ago`
  return iso.slice(0, 10)
}

const PAGE_SIZE_OPTIONS = [10, 20, 50]

// ---- component -------------------------------------------------------------

export default function TerminalList() {
  const navigate = useNavigate()

  // ---- state ---------------------------------------------------------------
  const [data, setData] = useState<TerminalSummary[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [filter, setFilter] = useState<'all' | 'online' | 'offline'>('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  // Keep latest filter/page/pageSize in refs for the WS callback.
  const filterRef = useRef(filter)
  filterRef.current = filter
  const pageRef = useRef(page)
  pageRef.current = page
  const pageSizeRef = useRef(pageSize)
  pageSizeRef.current = pageSize

  // ---- data fetching -------------------------------------------------------
  const fetchData = useCallback(
    async (f: 'all' | 'online' | 'offline', p: number, ps: number) => {
      setLoading(true)
      try {
        const req: { pageSize: number; page: number; onlineFilter?: boolean } =
          { pageSize: ps, page: p }
        if (f === 'online') req.onlineFilter = true
        else if (f === 'offline') req.onlineFilter = false

        const resp = await teamxClient.listTerminals(req)
        setData(resp.terminals)
        setTotal(resp.totalCount)
      } catch (e) {
        message.error(`Failed to load terminals: ${String(e)}`)
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  useEffect(() => {
    fetchData(filter, page, pageSize)
  }, [filter, page, pageSize, fetchData])

  // ---- WebSocket -----------------------------------------------------------
  const handleWsEvent = useCallback((event: WsEvent) => {
    setData((prev) => {
      const idx = prev.findIndex((t) => t.sessionId === event.session_id)
      if (idx === -1) {
        // Not on current page — refetch to stay in sync.
        fetchData(filterRef.current, pageRef.current, pageSizeRef.current)
        return prev
      }
      const updated = [...prev]
      updated[idx] = new TerminalSummary({
        ...updated[idx],
        online: event.type === 'online',
      })
      return updated
    })
  }, [])

  const { isConnected: wsConnected } = useWebSocket({ onEvent: handleWsEvent })

  // ---- actions -------------------------------------------------------------
  const refresh = useCallback(() => {
    fetchData(filter, page, pageSize)
  }, [filter, page, pageSize, fetchData])

  const handleDetail = useCallback(
    (sessionId: string) => navigate(`/terminal/${sessionId}`),
    [navigate],
  )

  const handleKick = useCallback((sessionId: string, hostname: string) => {
    Modal.confirm({
      title: `Kick ${hostname}?`,
      content: `Disconnect session ${shortId(sessionId)}. The client will exit permanently.`,
      okText: 'Kick',
      okType: 'danger',
      cancelText: 'Cancel',
      onOk: async () => {
        try {
          const resp = await teamxClient.disconnectTerminal({ sessionId })
          if (resp.ok) {
            message.success(`Kicked ${hostname}`)
          } else {
            message.warning(resp.message || 'Kick failed')
          }
        } catch (e) {
          message.error(`Kick failed: ${String(e)}`)
        }
        // Refetch regardless of outcome.
        fetchData(filterRef.current, pageRef.current, pageSizeRef.current)
      },
    })
  }, [])

  const handleBlock = useCallback((deviceId: string, hostname: string) => {
    Modal.confirm({
      title: `Block ${hostname}?`,
      content: `Device ${shortId(deviceId, 16)} will be blocked from re-registering. All active sessions will be kicked.`,
      okText: 'Block',
      okType: 'danger',
      cancelText: 'Cancel',
      onOk: async () => {
        try {
          const resp = await teamxClient.blockTerminal({ deviceId })
          if (resp.ok) {
            message.success(`Blocked ${hostname}`)
          } else {
            message.warning(resp.message || 'Block failed')
          }
        } catch (e) {
          message.error(`Block failed: ${String(e)}`)
        }
        fetchData(filterRef.current, pageRef.current, pageSizeRef.current)
      },
    })
  }, [])

  const handleUnblock = useCallback((deviceId: string, hostname: string) => {
    Modal.confirm({
      title: `Unblock ${hostname}?`,
      content: `Device ${shortId(deviceId, 16)} will be allowed to register again.`,
      okText: 'Unblock',
      cancelText: 'Cancel',
      onOk: async () => {
        try {
          const resp = await teamxClient.unblockTerminal({ deviceId })
          if (resp.ok) {
            message.success(`Unblocked ${hostname}`)
          } else {
            message.warning(resp.message || 'Unblock failed')
          }
        } catch (e) {
          message.error(`Unblock failed: ${String(e)}`)
        }
        fetchData(filterRef.current, pageRef.current, pageSizeRef.current)
      },
    })
  }, [])

  // ---- columns -------------------------------------------------------------
  const columns: ColumnsType<TerminalSummary> = [
    {
      title: 'Session ID',
      dataIndex: 'sessionId',
      key: 'sessionId',
      width: 160,
      render: (v: string) => (
        <Tooltip title={v}>
          <code style={{ fontSize: 12 }}>{shortId(v)}</code>
        </Tooltip>
      ),
    },
    {
      title: 'Hostname',
      dataIndex: 'hostname',
      key: 'hostname',
      sorter: (a, b) => a.hostname.localeCompare(b.hostname),
    },
    {
      title: 'OS',
      dataIndex: 'os',
      key: 'os',
      width: 110,
      render: (_os: string, r: TerminalSummary) =>
        r.osVersion ? `${r.os} ${r.osVersion}` : r.os,
    },
    {
      title: 'Client Ver',
      dataIndex: 'clientVersion',
      key: 'clientVersion',
      width: 100,
      render: (v: string) => v || '-',
    },
    {
      title: 'Status',
      dataIndex: 'online',
      key: 'online',
      width: 100,
      filters: [
        { text: 'Online', value: true },
        { text: 'Offline', value: false },
      ],
      onFilter: (value, record) => record.online === value,
      render: (online: boolean) => (
        <Tag color={online ? 'green' : 'red'}>
          {online ? 'Online' : 'Offline'}
        </Tag>
      ),
    },
    {
      title: 'Last Heartbeat',
      dataIndex: 'lastHeartbeat',
      key: 'lastHeartbeat',
      width: 130,
      sorter: (a, b) =>
        new Date(a.lastHeartbeat || 0).getTime() -
        new Date(b.lastHeartbeat || 0).getTime(),
      defaultSortOrder: 'descend',
      render: (v: string) => (
        <Tooltip title={v || 'never'}>
          <span>{timeAgo(v)}</span>
        </Tooltip>
      ),
    },
    {
      title: 'Actions',
      key: 'actions',
      width: 240,
      render: (_: unknown, record: TerminalSummary) => (
        <Space size={0} wrap>
          <Button
            type="link"
            icon={<EyeOutlined />}
            size="small"
            onClick={() => handleDetail(record.sessionId)}
          >
            Detail
          </Button>
          {record.online && (
            <Button
              type="link"
              icon={<StopOutlined />}
              size="small"
              danger
              onClick={() => handleKick(record.sessionId, record.hostname)}
            >
              Kick
            </Button>
          )}
          <Button
            type="link"
            icon={<LockOutlined />}
            size="small"
            onClick={() => handleBlock(record.deviceId, record.hostname)}
          >
            Block
          </Button>
          <Button
            type="link"
            icon={<UnlockOutlined />}
            size="small"
            onClick={() => handleUnblock(record.deviceId, record.hostname)}
          >
            Unblock
          </Button>
        </Space>
      ),
    },
  ]

  // ---- pagination ----------------------------------------------------------
  const pagination: TablePaginationConfig = {
    current: page,
    pageSize,
    total,
    showSizeChanger: true,
    pageSizeOptions: PAGE_SIZE_OPTIONS.map(String),
    showTotal: (t: number) => `${t} terminal${t !== 1 ? 's' : ''}`,
    onChange: (p: number, ps: number) => {
      if (p !== page) setPage(p)
      if (ps !== pageSize) setPageSize(ps)
    },
  }

  // ---- render --------------------------------------------------------------
  return (
    <div>
      <Typography.Title level={3}>Terminal List</Typography.Title>

      <Card>
        <div className="terminal-list-toolbar">
          <Radio.Group
            value={filter}
            onChange={(e) => {
              setFilter(e.target.value)
              setPage(1)
            }}
          >
            <Radio.Button value="all">All</Radio.Button>
            <Radio.Button value="online">Online</Radio.Button>
            <Radio.Button value="offline">Offline</Radio.Button>
          </Radio.Group>

          <Space>
            <Badge
              status={wsConnected ? 'success' : 'default'}
              text={wsConnected ? 'Live' : 'WS off'}
            />
            <Button
              icon={<ReloadOutlined />}
              loading={loading}
              onClick={refresh}
            >
              Refresh
            </Button>
          </Space>
        </div>

        <Table<TerminalSummary>
          columns={columns}
          dataSource={data}
          rowKey="sessionId"
          loading={loading}
          pagination={pagination}
          locale={{
            emptyText:
              'No terminals connected. Start a client to see data here.',
          }}
        />
      </Card>
    </div>
  )
}
