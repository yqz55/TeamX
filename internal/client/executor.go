package client

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"time"

	"teamx/internal/proto"

	"github.com/google/uuid"
)

// dispatchCommand handles a Command received from the server. It runs the
// appropriate handler and sends CommandResult(s) back on the stream.
func (c *Client) dispatchCommand(ctx context.Context, stream proto.TeamX_ChannelClient, cmd *proto.Command) {
	commandID := cmd.GetCommandId()
	cmdType := cmd.GetType()

	log.Printf("[executor] received: command_id=%s type=%s", commandID, cmdType.String())

	// Send Executing status so the server knows we started.
	c.sendCommandResult(stream, commandID, "Executing", 0, "", "", "",
		time.Now().Unix(), 0)

	switch cmdType {
	case proto.CommandType_COMMAND_TYPE_COLLECT_NOW:
		c.handleCollectNow(ctx, stream, cmd)

	case proto.CommandType_COMMAND_TYPE_RUN_SCRIPT:
		c.handleRunScript(ctx, stream, cmd)

	default:
		c.sendCommandResult(stream, commandID, "Failed", -1, "", "",
			fmt.Sprintf("unsupported command type: %s", cmdType.String()),
			time.Now().Unix(), time.Now().Unix())
	}
}

// ---- CollectNow ------------------------------------------------------------

func (c *Client) handleCollectNow(ctx context.Context, stream proto.TeamX_ChannelClient, cmd *proto.Command) {
	commandID := cmd.GetCommandId()

	// Collect hardware info immediately.
	hwInfo := c.col.CollectHardware()
	if hwInfo == nil {
		c.sendCommandResult(stream, commandID, "Failed", -1, "", "",
			"hardware collection returned nil", time.Now().Unix(), time.Now().Unix())
		return
	}

	// Dedup check.
	if c.cache.IsChanged(hwInfo) {
		reportID := uuid.New().String()
		msg := &proto.ClientMessage{
			Seq: c.nextSeq(),
			Payload: &proto.ClientMessage_ReportRequest{
				ReportRequest: &proto.ReportRequest{
					ReportId: reportID,
					Type:     &proto.ReportRequest_Hardware{Hardware: hwInfo},
				},
			},
		}
		if err := stream.Send(msg); err != nil {
			log.Printf("[executor] collect_now report send failed: command_id=%s err=%v", commandID, err)
			c.sendCommandResult(stream, commandID, "Failed", -1, "", "",
				fmt.Sprintf("send report failed: %v", err),
				time.Now().Unix(), time.Now().Unix())
			return
		}
		c.cache.MarkSent(hwInfo)
		log.Printf("[executor] collect_now: report sent report_id=%s", reportID)
	} else {
		log.Printf("[executor] collect_now: no change, skipping report")
	}

	c.sendCommandResult(stream, commandID, "Completed", 0, "hardware report triggered", "", "",
		time.Now().Unix(), time.Now().Unix())
}

// ---- RunScript -------------------------------------------------------------

func (c *Client) handleRunScript(ctx context.Context, stream proto.TeamX_ChannelClient, cmd *proto.Command) {
	commandID := cmd.GetCommandId()
	script := cmd.GetParams()["cmd"]
	if script == "" {
		c.sendCommandResult(stream, commandID, "Failed", -1, "", "",
			"missing required param: cmd", time.Now().Unix(), time.Now().Unix())
		return
	}

	// Build the exec command.
	var shell, shellFlag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shellFlag = "/c"
	} else {
		shell = "sh"
		shellFlag = "-c"
	}

	timeout := time.Duration(cmd.GetTimeoutSec()) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startedAt := time.Now()
	proc := exec.CommandContext(execCtx, shell, shellFlag, script)
	stdout, err := proc.Output() // captures stdout, waits for completion

	finishedAt := time.Now()
	exitCode := int32(0)
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			c.sendCommandResult(stream, commandID, "Failed", -1, "", "",
				"command timed out", startedAt.Unix(), finishedAt.Unix())
			return
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = -1
		}
	}

	stderr := ""
	if exitCode != 0 && err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		} else {
			stderr = err.Error()
		}
	}

	status := "Completed"
	if exitCode != 0 {
		status = "Failed"
	}

	log.Printf("[executor] run_script: command_id=%s exit=%d elapsed=%v",
		commandID, exitCode, finishedAt.Sub(startedAt))

	c.sendCommandResult(stream, commandID, status, exitCode, string(stdout), stderr, "",
		startedAt.Unix(), finishedAt.Unix())
}

// ---- helpers ----------------------------------------------------------------

func (c *Client) sendCommandResult(stream proto.TeamX_ChannelClient, commandID, status string,
	exitCode int32, stdout, stderr, errorMsg string, startedAt, finishedAt int64) {

	result := &proto.CommandResult{
		CommandId:      commandID,
		Status:         status,
		ExitCode:       exitCode,
		Stdout:         stdout,
		Stderr:         stderr,
		ErrorMessage:   errorMsg,
		StartedAtUnix:  startedAt,
		FinishedAtUnix: finishedAt,
	}

	msg := &proto.ClientMessage{
		Seq: c.nextSeq(),
		Payload: &proto.ClientMessage_CommandResult{
			CommandResult: result,
		},
	}

	if err := stream.Send(msg); err != nil {
		log.Printf("[executor] send result failed: command_id=%s err=%v", commandID, err)
	}
}
