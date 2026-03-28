package app

import (
	"fmt"
	"io"
	"time"

	"github.com/silvekt/scriptorium/internal/config"
	"github.com/silvekt/scriptorium/internal/guide"
	"github.com/silvekt/scriptorium/internal/protocol"
)

func ExecuteGuideImplementation(cfg config.Runtime, input guide.Request, stderr io.Writer) protocol.ToolResult {
	started := time.Now()
	logger := newLogger(cfg.ServerName, stderr)

	response, err := guide.Execute(cfg, input)
	elapsedMs := time.Since(started).Milliseconds()
	if err != nil {
		logger.Printf("tool=guide_implementation topic=%q error=%v elapsedMs=%d", input.Topic, err, elapsedMs)
		return protocol.CreateToolErrorResult("guide_implementation failed", map[string]any{
			"tool":    "guide_implementation",
			"message": err.Error(),
		})
	}

	logger.Printf(
		"tool=guide_implementation topic=%q docs=%d code=%d confidence=%s elapsedMs=%d",
		input.Topic,
		len(response.Docs),
		len(response.SampleCode),
		response.Confidence,
		elapsedMs,
	)
	return protocol.CreateToolResult(
		response,
		fmt.Sprintf("Built implementation guide with %d supporting ref(s).", len(response.Docs)+len(response.SampleCode)),
	)
}
