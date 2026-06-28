package server

import (
	"context"
	"database/sql"
	"io"
	"log"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"teamx/internal/proto"
	"teamx/internal/server/store"

	"github.com/google/uuid"
)

// TeamXServer implements the gRPC TeamX service.
type TeamXServer struct {
	proto.UnimplementedTeamXServer
	cm    *ConnectionManager
	store store.Store
}

// NewTeamXServer creates a new server with the given dependencies.
func NewTeamXServer(cm *ConnectionManager, store store.Store) *TeamXServer {
	return &TeamXServer{cm: cm, store: store}
}

// Register handles the client handshake. It assigns a unique session_id and
// stores the client in the ConnectionManager (offline until the Channel stream
// is opened).
func (s *TeamXServer) Register(ctx context.Context, req *proto.HandshakeRequest) (*proto.HandshakeResponse, error) {
	if s.cm.IsFull() {
		log.Printf("[register] rejected: server full hostname=%s", req.GetHostname())
		return nil, status.Error(codes.ResourceExhausted, "server at capacity")
	}

	deviceID := req.GetDeviceId()
	if blocked, err := s.store.IsDeviceBlocked(deviceID); err != nil {
		log.Printf("[register] blocklist check failed: device=%s err=%v", deviceID, err)
	} else if blocked {
		log.Printf("[register] rejected: device=%s is blocked", deviceID)
		return &proto.HandshakeResponse{
			Ok:      false,
			Message: "device is blocked",
		}, nil
	}

	sessionID := uuid.New().String()

	conn := &ClientConn{
		SessionID:     sessionID,
		DeviceID:      deviceID,
		Hostname:      req.GetHostname(),
		OS:            req.GetOs(),
		OSVersion:     req.GetOsVersion(),
		ClientVersion: req.GetClientVersion(),
		MacAddrs:      req.GetMacAddrs(),
		IPAddrs:       req.GetIpAddrs(),
		ConnectedAt:   time.Now(),
	}
	s.cm.Add(conn)

	// Log with truncated device_id for readability — do NOT mutate deviceID.
	devLog := deviceID
	if len(devLog) > 16 {
		devLog = deviceID[:16]
	}
	log.Printf("[register] session=%s device=%s hostname=%s os=%s version=%s",
		sessionID, devLog, conn.Hostname, conn.OS, conn.ClientVersion)

	// Persist to store using full deviceID, not the truncated log copy.
	if err := s.store.UpsertTerminal(sessionID, deviceID, req.GetHostname(), req.GetOs(),
		req.GetOsVersion(), req.GetKernelVersion(), req.GetClientVersion(),
		req.GetMacAddrs(), req.GetIpAddrs()); err != nil {
		log.Printf("[register] store upsert failed: session=%s err=%v", sessionID, err)
	}

	return &proto.HandshakeResponse{
		Ok:         true,
		SessionId:  sessionID,
		ServerTime: time.Now().Format(time.RFC3339),
		Message:    "welcome to TeamX",
	}, nil
}

type recvResult struct {
	msg *proto.ClientMessage
	err error
}

func (s *TeamXServer) Channel(stream proto.TeamX_ChannelServer) error {
	sessionID, err := extractSessionID(stream.Context())
	if err != nil {
		return err
	}

	conn := s.cm.Get(sessionID)
	if conn == nil {
		return status.Errorf(codes.NotFound, "session %s not found — call Register first", sessionID)
	}

	s.cm.SetStream(sessionID, stream)
	defer func() {
		s.cm.ClearStream(sessionID)
		if err := s.store.MarkOffline(sessionID); err != nil {
			log.Printf("[channel] store mark offline failed: session=%s err=%v", sessionID, err)
		}
	}()

	log.Printf("[channel] stream opened: session=%s", sessionID)

	msgCh := make(chan recvResult, 8)
	streamCtx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	go func() {
		for {
			msg, err := stream.Recv()
			select {
			case msgCh <- recvResult{msg, err}:
			case <-streamCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-conn.DisconnectCh:
			log.Printf("[channel] admin kick: session=%s", sessionID)
			return status.Error(codes.PermissionDenied, "kicked by admin")

		case r := <-msgCh:
			if r.err == io.EOF {
				log.Printf("[channel] stream closed (EOF): session=%s", sessionID)
				return nil
			}
			if r.err != nil {
				log.Printf("[channel] stream error: session=%s err=%v", sessionID, r.err)
				return r.err
			}

			switch payload := r.msg.Payload.(type) {
			case *proto.ClientMessage_Heartbeat:
				s.handleHeartbeat(stream, sessionID, payload.Heartbeat)
			case *proto.ClientMessage_ReportRequest:
				s.handleReport(sessionID, conn.DeviceID, payload.ReportRequest)
			case *proto.ClientMessage_CommandResult:
				s.handleCommandResult(sessionID, payload.CommandResult)
			default:
				log.Printf("[channel] unknown message type from session=%s seq=%d", sessionID, r.msg.Seq)
			}
		}
	}
}

func (s *TeamXServer) handleHeartbeat(stream proto.TeamX_ChannelServer, sessionID string, hb *proto.Heartbeat) {
	s.cm.RecordHeartbeat(sessionID)
	if err := s.store.UpdateHeartbeat(sessionID); err != nil {
		log.Printf("[heartbeat] store update failed: session=%s err=%v", sessionID, err)
	}
	ack := &proto.ServerMessage{
		Seq: 0,
		Payload: &proto.ServerMessage_HeartbeatAck{
			HeartbeatAck: &proto.HeartbeatAck{ServerTimeUnix: time.Now().Unix()},
		},
	}
	if err := stream.Send(ack); err != nil {
		log.Printf("[heartbeat] send ack failed: session=%s err=%v", sessionID, err)
	}
}

func (s *TeamXServer) handleReport(sessionID, deviceID string, report *proto.ReportRequest) {
	switch payload := report.Type.(type) {
	case *proto.ReportRequest_Hardware:
		hw := payload.Hardware
		cpu := hw.GetCpu()
		mem := hw.GetMemory()
		devLog := deviceID
		if len(devLog) > 16 {
			devLog = deviceID[:16]
		}
		log.Printf("[report] hardware: session=%s device=%s report_id=%s cpu=%s cores=%d/%d arch=%s mem=%dMB/%dMB disks=%d nets=%d bios=%v mb=%v",
			sessionID, devLog, report.GetReportId(),
			cpu.GetModel(), cpu.GetCores(), cpu.GetThreads(), cpu.GetArchitecture(),
			mem.GetUsedBytes()/(1024*1024), mem.GetTotalBytes()/(1024*1024),
			len(hw.GetDisks()), len(hw.GetNets()),
			hw.GetBios() != nil, hw.GetMotherboard() != nil,
		)
		if err := s.store.SaveHardwareReport(sessionID, deviceID, report); err != nil {
			log.Printf("[report] store save failed: session=%s report_id=%s err=%v",
				sessionID, report.GetReportId(), err)
		}
	default:
		log.Printf("[report] session=%s report_id=%s type=<unknown>", sessionID, report.GetReportId())
	}
}

func (s *TeamXServer) handleCommandResult(sessionID string, result *proto.CommandResult) {
	log.Printf("[command] result: session=%s command_id=%s status=%s", sessionID, result.GetCommandId(), result.GetStatus())
}

func (s *TeamXServer) ListTerminals(ctx context.Context, req *proto.ListTerminalsRequest) (*proto.ListTerminalsResponse, error) {
	var online *bool
	if req.OnlineFilter != nil {
		online = req.OnlineFilter
	}
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 50
	}
	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	terminals, total, err := s.store.ListTerminals(online, offset, pageSize)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list terminals failed: %v", err)
	}
	summaries := make([]*proto.TerminalSummary, len(terminals))
	for i, t := range terminals {
		summaries[i] = &proto.TerminalSummary{
			SessionId:     t.SessionID,
			DeviceId:      t.DeviceID,
			Hostname:      t.Hostname,
			Os:            t.OS,
			OsVersion:     t.OSVersion,
			ClientVersion: t.ClientVersion,
			Online:        t.Online,
			LastHeartbeat: t.LastHeartbeat,
			LastSeenAt:    t.LastSeenAt,
		}
	}
	return &proto.ListTerminalsResponse{Terminals: summaries, TotalCount: int32(total)}, nil
}

func (s *TeamXServer) GetTerminal(ctx context.Context, req *proto.GetTerminalRequest) (*proto.GetTerminalResponse, error) {
	var t *store.Terminal
	var err error
	if req.GetDeviceId() != "" {
		t, err = s.store.GetTerminalByDevice(req.GetDeviceId())
	} else {
		t, err = s.store.GetTerminal(req.GetSessionId())
	}
	if err == sql.ErrNoRows {
		return nil, status.Errorf(codes.NotFound, "terminal not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get terminal failed: %v", err)
	}
	hw, err := s.store.GetLatestHardware(t.DeviceID)
	if err != nil {
		log.Printf("[query] get latest hardware failed: device=%s err=%v", t.DeviceID, err)
	}
	return &proto.GetTerminalResponse{
		Summary: &proto.TerminalSummary{
			SessionId:     t.SessionID,
			DeviceId:      t.DeviceID,
			Hostname:      t.Hostname,
			Os:            t.OS,
			OsVersion:     t.OSVersion,
			ClientVersion: t.ClientVersion,
			Online:        t.Online,
			LastHeartbeat: t.LastHeartbeat,
			LastSeenAt:    t.LastSeenAt,
		},
		LatestHardware: hw,
	}, nil
}

func (s *TeamXServer) GetTerminalHistory(ctx context.Context, req *proto.GetTerminalHistoryRequest) (*proto.GetTerminalHistoryResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	snaps, err := s.store.ListHardwareReports(req.GetDeviceId(), req.GetSince(), req.GetUntil(), limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list hardware reports failed: %v", err)
	}
	snapshots := make([]*proto.HardwareSnapshot, len(snaps))
	for i, s := range snaps {
		snapshots[i] = &proto.HardwareSnapshot{ReportId: s.ReportID, CreatedAt: s.CreatedAt, Info: s.Info}
	}
	return &proto.GetTerminalHistoryResponse{DeviceId: req.GetDeviceId(), Snapshots: snapshots}, nil
}

func (s *TeamXServer) DisconnectTerminal(ctx context.Context, req *proto.DisconnectTerminalRequest) (*proto.DisconnectTerminalResponse, error) {
	conn := s.cm.Get(req.GetSessionId())
	if conn == nil || !conn.Online {
		return &proto.DisconnectTerminalResponse{Ok: false, Message: "session not found or offline"}, nil
	}
	s.cm.Kick(req.GetSessionId())
	log.Printf("[admin] kick: session=%s host=%s", req.GetSessionId(), conn.Hostname)
	return &proto.DisconnectTerminalResponse{Ok: true, Message: "kicked"}, nil
}

func (s *TeamXServer) BlockTerminal(ctx context.Context, req *proto.BlockTerminalRequest) (*proto.BlockTerminalResponse, error) {
	if err := s.store.MarkBlocked(req.GetDeviceId()); err != nil {
		return nil, status.Errorf(codes.Internal, "block failed: %v", err)
	}
	s.kickDeviceSessions(req.GetDeviceId())
	log.Printf("[admin] block: device=%s", req.GetDeviceId())
	return &proto.BlockTerminalResponse{Ok: true, Message: "blocked"}, nil
}

func (s *TeamXServer) UnblockTerminal(ctx context.Context, req *proto.UnblockTerminalRequest) (*proto.UnblockTerminalResponse, error) {
	if err := s.store.UnblockTerminal(req.GetDeviceId()); err != nil {
		return nil, status.Errorf(codes.Internal, "unblock failed: %v", err)
	}
	log.Printf("[admin] unblock: device=%s", req.GetDeviceId())
	return &proto.UnblockTerminalResponse{Ok: true, Message: "unblocked"}, nil
}

func (s *TeamXServer) kickDeviceSessions(deviceID string) {
	s.cm.mu.Lock()
	defer s.cm.mu.Unlock()
	for id, conn := range s.cm.sessions {
		if conn.DeviceID == deviceID && conn.Online {
			select {
			case <-conn.DisconnectCh:
			default:
				close(conn.DisconnectCh)
			}
			devLog := deviceID
			if len(devLog) > 16 {
				devLog = deviceID[:16]
			}
			log.Printf("[admin] kick: session=%s (blocked device=%s)", id, devLog)
		}
	}
}

func extractSessionID(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "metadata required — provide session-id")
	}
	ids := md.Get("session-id")
	if len(ids) == 0 {
		return "", status.Error(codes.Unauthenticated, "session-id header required")
	}
	return ids[0], nil
}

func (s *TeamXServer) TransferFile(stream proto.TeamX_TransferFileServer) error {
	return status.Error(codes.Unimplemented, "TransferFile is not yet implemented (Phase 7)")
}

var _ proto.TeamXServer = (*TeamXServer)(nil)
