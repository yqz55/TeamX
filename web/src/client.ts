import { createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { TeamX } from './gen/teamx_connect'

const baseUrl = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080'

const transport = createConnectTransport({
  baseUrl,
})

/**
 * Type-safe ConnectRPC client for the TeamX admin service.
 *
 * Usage:
 *   import { teamxClient } from '@/client'
 *   const resp = await teamxClient.listTerminals({ pageSize: 50, page: 1 })
 */
export const teamxClient = createClient(TeamX, transport)
