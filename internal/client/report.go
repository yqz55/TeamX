package client

import (
	"context"
	"log"
	"time"

	"teamx/internal/proto"

	"github.com/google/uuid"
)

// reportLoop periodically collects hardware info and sends a ReportRequest
// via the Channel stream when the data has changed since the last report.
// It runs until ctx is cancelled, then signals done and returns.
func (c *Client) reportLoop(ctx context.Context, stream proto.TeamX_ChannelClient, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(c.cfg.ReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		info := c.col.CollectHardware()
		if !c.cache.IsChanged(info) {
			continue
		}

		reportID := uuid.New().String()
		msg := &proto.ClientMessage{
			Seq: c.nextSeq(),
			Payload: &proto.ClientMessage_ReportRequest{
				ReportRequest: &proto.ReportRequest{
					ReportId: reportID,
					Type: &proto.ReportRequest_Hardware{
						Hardware: info,
					},
				},
			},
		}

		if err := stream.Send(msg); err != nil {
			log.Printf("[client] report send failed: %v", err)
			return
		}

		c.cache.MarkSent(info)
		log.Printf("[client] hardware report sent: id=%s cpu=%s cores=%d mem=%dMB",
			reportID, info.GetCpu().GetModel(), info.GetCpu().GetCores(),
			info.GetMemory().GetTotalBytes()/(1024*1024))
	}
}
