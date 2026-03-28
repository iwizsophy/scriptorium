package app

import (
	"fmt"
	"io"
	"time"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/flow"
	"github.com/silvekt/scriptorium/internal/protocol"
)

func ExecuteSummarizeFlow(cfg config.Runtime, input flow.Request, stderr io.Writer) protocol.ToolResult {
	started := time.Now()
	logger := newLogger(cfg.ServerName, stderr)

	response, err := flow.Execute(cfg, input)
	elapsedMs := time.Since(started).Milliseconds()
	if err != nil {
		logger.Printf("tool=summarize_flow refs=%d error=%v elapsedMs=%d", len(input.Seeds)+len(input.Sources), err, elapsedMs)
		return protocol.CreateToolErrorResult("summarize_flow failed", map[string]any{
			"tool":    "summarize_flow",
			"message": err.Error(),
		})
	}

	logger.Printf(
		"tool=summarize_flow refs=%d steps=%d elapsedMs=%d",
		len(input.Seeds)+len(input.Sources),
		len(response.Flow),
		elapsedMs,
	)
	return protocol.CreateToolResult(
		response,
		fmt.Sprintf("Built %d flow step(s).", len(response.Flow)),
	)
}
