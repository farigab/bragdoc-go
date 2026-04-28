// Package integration contains HTTP clients integrating with external services.
package integration

import (
	"io"

	"bragdev-go/internal/logger"
)

// closeBody closes an io.Closer and logs any error using the application logger.
func closeBody(c io.Closer) {
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		logger.Errorw("integration: failed to close response body", "err", err)
	}
}
