package app

import (
	"fmt"
	"io"
	"time"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/protocol"
	"github.com/silvekt/scriptorium/internal/related"
)

func ExecuteExpandRelated(cfg config.Runtime, input related.Request, stderr io.Writer) protocol.ToolResult {
	started := time.Now()
	logger := newLogger(cfg.ServerName, stderr)

	response, err := related.Execute(cfg, input)
	elapsedMs := time.Since(started).Milliseconds()
	if err != nil {
		logger.Printf("tool=expand_related seeds=%d error=%v elapsedMs=%d", len(input.Seeds), err, elapsedMs)
		return protocol.CreateToolErrorResult("expand_related failed", map[string]any{
			"tool":    "expand_related",
			"message": err.Error(),
		})
	}

	logger.Printf(
		"tool=expand_related seeds=%d results=%d elapsedMs=%d",
		len(input.Seeds),
		len(response.Related),
		elapsedMs,
	)
	return protocol.CreateToolResult(
		response,
		fmt.Sprintf("Expanded %d related reference(s).", len(response.Related)),
	)
}
