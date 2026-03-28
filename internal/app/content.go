package app

import (
	"fmt"
	"io"
	"time"

	"github.com/iwizsophy/scriptorium/internal/config"
	"github.com/iwizsophy/scriptorium/internal/content"
	"github.com/iwizsophy/scriptorium/internal/protocol"
)

func ExecuteGetContent(cfg config.Runtime, input content.Request, stderr io.Writer) protocol.ToolResult {
	started := time.Now()
	logger := newLogger(cfg.ServerName, stderr)

	response, err := content.Execute(cfg, input)
	elapsedMs := time.Since(started).Milliseconds()
	if err != nil {
		logger.Printf("tool=get_content refId=%q error=%v elapsedMs=%d", input.RefID, err, elapsedMs)
		return protocol.CreateToolErrorResult("get_content failed", map[string]any{
			"tool":    "get_content",
			"message": err.Error(),
		})
	}

	logger.Printf(
		"tool=get_content refId=%q mode=%q elapsedMs=%d",
		input.RefID,
		response.Mode,
		elapsedMs,
	)
	return protocol.CreateToolResult(
		response,
		fmt.Sprintf("Loaded %s content.", response.Kind),
	)
}
