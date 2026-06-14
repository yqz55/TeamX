package server

import (
	"context"
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

// Register handles the client handshake. It assigns a unique client_id and
// stores the client in the ConnectionManager (offline until the Channel stream
// is opened).
func (s *TeamXServer) Register(ctx context.Context, req *proto.HandshakeRequest) (*proto.HandshakeResponse, error) {
	clientID := uuid.New().String()

	conn := &ClientConn{
		ID:            clientID,
		Hostname:      req.GetHostname(),
		OS:            req.GetOs(),
		OSVersion:     req.GetOsVersion(),
		ClientVersion: req.GetClientVersion(),
		MacAddrs:      req.GetMacAddrs(),
		IPAddrs:       req.GetIpAddrs(),
		ConnectedAt:   time.Now(),
	}
	s.cm.Add(conn)

	log.Printf("[register] client registered: id=%s hostname=%s os=%s version=%s",
		clientID, conn.Hostname, conn.OS, conn.ClientVersion)

	// Persist to store — failure does not block registration.
	if err := s.store.UpsertTerminal(clientID, req.GetHostname(), req.GetOs(),
		req.GetOsVersion(), req.GetKernelVersion(), req.GetClientVersion(),
		req.GetMacAddrs(), req.GetIpAddrs()); err != nil {
		log.Printf("[register] store upsert failed: client=%s err=%v", clientID, err)
	}

	return &proto.HandshakeResponse{
		Ok:        true,
		ClientId:  clientID,
		ServerTime: time.Now().Format(time.RFC3339),
		Message:   "welcome to TeamX",
	}, nil
}

// Channel handles the bidirectional stream between server and a client. The
// client MUST include its client_id via gRPC metadata ("client-id").
func (s *TeamXServer) Channel(stream proto.TeamX_ChannelServer) error {
	// Extract client_id from metadata.
	clientID, err := extractClientID(stream.Context())
	if err != nil {
		return err
	}

	// Validate the client exists.
	conn := s.cm.Get(clientID)
	if conn == nil {
		return status.Errorf(codes.NotFound, "client %s not found — call Register first", clientID)
	}

	// Bind this stream to the client.
	s.cm.SetStream(clientID, stream)
	defer s.cm.ClearStream(clientID)

	log.Printf("[channel] stream opened: client=%s", clientID)

	// Recv loop — process incoming messages from the client.
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Printf("[channel] stream closed (EOF): client=%s", clientID)
			return nil
		}
		if err != nil {
			log.Printf("[channel] stream error: client=%s err=%v", clientID, err)
			return err
		}

		switch payload := msg.Payload.(type) {
		case *proto.ClientMessage_Heartbeat:
			s.handleHeartbeat(stream, clientID, payload.Heartbeat)
		case *proto.ClientMessage_ReportRequest:
			s.handleReport(clientID, payload.ReportRequest)
		case *proto.ClientMessage_CommandResult:
			s.handleCommandResult(clientID, payload.CommandResult)
		default:
			log.Printf("[channel] unknown message type from client=%s seq=%d", clientID, msg.Seq)
		}
	}
}

// ---- message handlers -------------------------------------------------------

func (s *TeamXServer) handleHeartbeat(stream proto.TeamX_ChannelServer, clientID string, hb *proto.Heartbeat) {
	s.cm.RecordHeartbeat(clientID)

	// Persist heartbeat — not critical path; log and continue on failure.
	if err := s.store.UpdateHeartbeat(clientID); err != nil {
		log.Printf("[heartbeat] store update failed: client=%s err=%v", clientID, err)
	}

	ack := &proto.ServerMessage{
		Seq: 0, // Phase 1: seq is placeholder
		Payload: &proto.ServerMessage_HeartbeatAck{
			HeartbeatAck: &proto.HeartbeatAck{
				ServerTimeUnix: time.Now().Unix(),
			},
		},
	}

	if err := stream.Send(ack); err != nil {
		log.Printf("[heartbeat] send ack failed: client=%s err=%v", clientID, err)
	}
}

func (s *TeamXServer) handleReport(clientID string, report *proto.ReportRequest) {
	switch payload := report.Type.(type) {
	case *proto.ReportRequest_Hardware:
		hw := payload.Hardware
		cpu := hw.GetCpu()
		mem := hw.GetMemory()
		log.Printf("[report] hardware: client=%s report_id=%s cpu=%s cores=%d/%d arch=%s mem=%dMB/%dMB disks=%d nets=%d bios=%v mb=%v",
			clientID, report.GetReportId(),
			cpu.GetModel(), cpu.GetCores(), cpu.GetThreads(), cpu.GetArchitecture(),
			mem.GetUsedBytes()/(1024*1024), mem.GetTotalBytes()/(1024*1024),
			len(hw.GetDisks()), len(hw.GetNets()),
			hw.GetBios() != nil, hw.GetMotherboard() != nil,
		)

		// Persist hardware report — failure does not block the stream.
		if err := s.store.SaveHardwareReport(clientID, report); err != nil {
			log.Printf("[report] store save failed: client=%s report_id=%s err=%v",
				clientID, report.GetReportId(), err)
		}

	default:
		log.Printf("[report] client=%s report_id=%s type=<unknown>", clientID, report.GetReportId())
	}
}

func (s *TeamXServer) handleCommandResult(clientID string, result *proto.CommandResult) {
	// Phase 5 will process command results. For now, just log.
	log.Printf("[command] result: client=%s command_id=%s status=%s", clientID, result.GetCommandId(), result.GetStatus())
}

// ---- helpers ----------------------------------------------------------------

func extractClientID(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "metadata required — provide client-id")
	}
	ids := md.Get("client-id")
	if len(ids) == 0 {
		return "", status.Error(codes.Unauthenticated, "client-id header required")
	}
	return ids[0], nil
}

// TransferFile is stubbed — it will be implemented in Phase 7.
func (s *TeamXServer) TransferFile(stream proto.TeamX_TransferFileServer) error {
	return status.Error(codes.Unimplemented, "TransferFile is not yet implemented (Phase 7)")
}

// Ensure interface compliance.
var _ proto.TeamXServer = (*TeamXServer)(nil)
