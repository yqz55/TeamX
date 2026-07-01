package server

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"teamx/internal/proto"

	"github.com/google/uuid"
)

const defaultCommandTimeout = 30 * time.Second

// ---- SendCommand (Admin RPC) -----------------------------------------------

func (s *TeamXServer) SendCommand(ctx context.Context, req *proto.SendCommandRequest) (*proto.SendCommandResponse, error) {
	deviceID := req.GetDeviceId()
	if deviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	cmdType := req.GetType()
	if cmdType == proto.CommandType_COMMAND_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "command type is required")
	}

	// Find the terminal's current session.
	t, err := s.store.GetTerminalByDevice(deviceID)
	if err != nil {
		return &proto.SendCommandResponse{
			Ok:      false,
			Message: "device not found",
		}, nil
	}

	// Check online status and queue existence.
	conn := s.cm.Get(t.SessionID)
	if conn == nil || !conn.Online {
		return &proto.SendCommandResponse{
			Ok:      false,
			Message: "terminal is offline",
		}, nil
	}

	// Generate command ID and persist.
	commandID := uuid.New().String()
	params := req.GetParams()
	if params == nil {
		params = make(map[string]string)
	}
	if err := s.store.SaveCommandLog(commandID, t.SessionID, deviceID, cmdType, params); err != nil {
		log.Printf("[command] store save failed: command_id=%s err=%v", commandID, err)
		return nil, status.Errorf(codes.Internal, "save command log failed: %v", err)
	}

	// Build the outgoing Command message.
	timeoutSec := req.GetTimeoutSec()
	if timeoutSec <= 0 {
		timeoutSec = int64(defaultCommandTimeout.Seconds())
	}
	cmd := &proto.Command{
		CommandId:     commandID,
		Type:          cmdType,
		Params:        params,
		TimeoutSec:    timeoutSec,
		CreatedAtUnix: time.Now().Unix(),
	}

	// Non-blocking enqueue. The Channel's consumer goroutine drains the queue
	// serially, pushing each command to the stream and starting its timeout.
	select {
	case conn.CmdQueue <- cmd:
		// queued successfully
	default:
		_ = s.store.UpdateCommandStatus(commandID, "Failed")
		return &proto.SendCommandResponse{
			Ok:      false,
			CommandId: commandID,
			Message: "command queue full — retry later",
		}, nil
	}

	devLog := deviceID
	if len(devLog) > 16 {
		devLog = deviceID[:16]
	}
	log.Printf("[command] queued: command_id=%s device=%s type=%s", commandID, devLog, cmdType.String())

	return &proto.SendCommandResponse{
		Ok:        true,
		CommandId: commandID,
		Message:   "queued",
	}, nil
}

// commandConsumer drains the per-terminal command queue and pushes each command
// to the Channel stream. It runs until ctx is cancelled, then closes done.
//
// Serial consumption guarantees that commands are delivered in order even when
// multiple admins send commands concurrently via SendCommand.
func (s *TeamXServer) commandConsumer(ctx context.Context, stream proto.TeamX_ChannelServer,
	sessionID string, queue chan *proto.Command, done chan struct{}) {
	defer close(done)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[command] consumer exit: session=%s", sessionID)
			return
		case cmd := <-queue:
			serverMsg := &proto.ServerMessage{
				Payload: &proto.ServerMessage_Command{Command: cmd},
			}

			if err := stream.Send(serverMsg); err != nil {
				log.Printf("[command] consumer send failed: command_id=%s session=%s err=%v",
					cmd.GetCommandId(), sessionID, err)
				_ = s.store.UpdateCommandStatus(cmd.GetCommandId(), "Failed")
				continue
			}

			// Mark as Sent.
			if err := s.store.UpdateCommandStatus(cmd.GetCommandId(), "Sent"); err != nil {
				log.Printf("[command] update status Sent failed: command_id=%s err=%v",
					cmd.GetCommandId(), err)
			}

			log.Printf("[command] sent: command_id=%s session=%s type=%s",
				cmd.GetCommandId(), sessionID[:min(8, len(sessionID))], cmd.GetType().String())

			// Start timeout watcher.
			timeout := time.Duration(cmd.GetTimeoutSec()) * time.Second
			if timeout <= 0 {
				timeout = defaultCommandTimeout
			}
			go s.watchCommandTimeout(cmd.GetCommandId(), timeout)
		}
	}
}

// watchCommandTimeout monitors a command and marks it Timeout if it hasn't
// completed within the deadline. Uses MarkCommandTimeout which is a no-op
// when the command already reached a terminal state (Completed/Failed).
func (s *TeamXServer) watchCommandTimeout(commandID string, timeout time.Duration) {
	<-time.After(timeout)
	if err := s.store.MarkCommandTimeout(commandID); err != nil {
		log.Printf("[command] timeout mark failed: command_id=%s err=%v", commandID, err)
		return
	}
	log.Printf("[command] timeout: command_id=%s", commandID)
}

// ---- GetCommandLog (Admin RPC) ---------------------------------------------

func (s *TeamXServer) GetCommandLog(ctx context.Context, req *proto.GetCommandLogRequest) (*proto.GetCommandLogResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	entries, err := s.store.GetCommandLog(req.GetDeviceId(), req.GetSessionId(), limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get command log failed: %v", err)
	}

	pbEntries := make([]*proto.CommandLogEntry, len(entries))
	for i, e := range entries {
		pbEntries[i] = &proto.CommandLogEntry{
			CommandId:    e.CommandID,
			SessionId:    e.SessionID,
			DeviceId:     e.DeviceID,
			Type:         e.Type,
			Params:       e.Params,
			Status:       e.Status,
			ExitCode:     e.ExitCode,
			Stdout:       e.Stdout,
			Stderr:       e.Stderr,
			ErrorMessage: e.ErrorMessage,
			CreatedAt:    e.CreatedAt,
			StartedAt:    e.StartedAt,
			FinishedAt:   e.FinishedAt,
		}
	}

	return &proto.GetCommandLogResponse{Entries: pbEntries}, nil
}

// ---- handleCommandResult (called from Channel select loop) ------------------

func (s *TeamXServer) handleCommandResult(sessionID string, result *proto.CommandResult) {
	log.Printf("[command] result: session=%s command_id=%s status=%s exit=%d",
		sessionID, result.GetCommandId(), result.GetStatus(), result.GetExitCode())

	// Persist the result to command_logs.
	if err := s.store.UpdateCommandResult(
		result.GetCommandId(),
		result.GetStatus(),
		result.GetExitCode(),
		result.GetStdout(),
		result.GetStderr(),
		result.GetErrorMessage(),
		formatUnixTime(result.GetStartedAtUnix()),
		formatUnixTime(result.GetFinishedAtUnix()),
	); err != nil {
		log.Printf("[command] store update result failed: command_id=%s err=%v",
			result.GetCommandId(), err)
	}
}

// ---- helpers ----------------------------------------------------------------

// formatUnixTime converts unix seconds to RFC 3339 string, or empty string for 0.
func formatUnixTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}
