import { useEffect, useState, useCallback, useRef } from 'react'

/** Event shape from the gateway WebSocket at /ws */
export interface WsEvent {
  type: 'online' | 'offline'
  session_id: string
  hostname: string
  timestamp: string // RFC 3339
}

interface UseWebSocketOptions {
  onEvent?: (event: WsEvent) => void
}

interface UseWebSocketReturn {
  isConnected: boolean
  lastEvent: WsEvent | null
  /** Force reconnect the WebSocket */
  reconnect: () => void
}

export function useWebSocket({
  onEvent,
}: UseWebSocketOptions = {}): UseWebSocketReturn {
  const [isConnected, setIsConnected] = useState(false)
  const [lastEvent, setLastEvent] = useState<WsEvent | null>(null)
  const [reconnectToken, setReconnectToken] = useState(0)

  // Keep callback in a ref so we don't reconnect WS when it changes identity
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const reconnect = useCallback(() => {
    setReconnectToken((t) => t + 1)
  }, [])

  useEffect(() => {
    const wsUrl = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws'
    const ws = new WebSocket(wsUrl)

    ws.onopen = () => setIsConnected(true)
    ws.onclose = () => setIsConnected(false)
    ws.onerror = () => setIsConnected(false)

    ws.onmessage = (msg: MessageEvent) => {
      try {
        const event: WsEvent = JSON.parse(msg.data as string)
        setLastEvent(event)
        onEventRef.current?.(event)
      } catch {
        // Ignore malformed messages
      }
    }

    return () => {
      ws.close()
    }
  }, [reconnectToken]) // onEvent no longer in deps — ref avoids stale closure

  return { isConnected, lastEvent, reconnect }
}
