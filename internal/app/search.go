package app

import (
	"fmt"
	"io"
	"time"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/protocol"
	"github.com/silvekt/scriptorium/internal/search"
)

func ExecuteSearch(cfg config.Runtime, input search.Request, stderr io.Writer) protocol.ToolResult {
	started := time.Now()
	logger := newLogger(cfg.ServerName, stderr)

	response, err := search.Execute(cfg, input)
	elapsedMs := time.Since(started).Milliseconds()
	if err != nil {
		logger.Printf("tool=search query=%q error=%v elapsedMs=%d", input.Query, err, elapsedMs)
		return protocol.CreateToolErrorResult("search failed", map[string]any{
			"tool":    "search",
			"message": err.Error(),
		})
	}

	logger.Printf(
		"tool=search query=%q results=%d elapsedMs=%d",
		input.Query,
		len(response.Results),
		elapsedMs,
	)
	return protocol.CreateToolResult(
		response,
		fmt.Sprintf("Found %d result(s).", len(response.Results)),
	)
}
