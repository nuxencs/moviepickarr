package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
)

func TestLogHTTPShutdownErrorClassifiesCause(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		message     string
		wantTimeout bool
	}{
		{
			name:        "deadline",
			err:         fmt.Errorf("drain: %w", context.DeadlineExceeded),
			message:     "http server did not drain before the shutdown timeout",
			wantTimeout: true,
		},
		{
			name:    "listener close",
			err:     errors.New("listener close failed"),
			message: "shutting down the http server failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logHTTPShutdownError(zerolog.New(&logs), tt.err)

			var line map[string]any
			if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
				t.Fatalf("decode log line %q: %v", logs.String(), err)
			}
			if got := line["message"]; got != tt.message {
				t.Errorf("message = %v, want %q", got, tt.message)
			}
			_, hasTimeout := line["timeout"]
			if hasTimeout != tt.wantTimeout {
				t.Errorf("timeout field present = %v, want %v: %v", hasTimeout, tt.wantTimeout, line)
			}
		})
	}
}
