// Package testutil provides shared test helpers for integration tests.
package testutil

import (
	"context"
	"database/sql"
	"time"
)

// PingWithRetry pings the database up to maxAttempts times, sleeping 1s
// between attempts. Returns nil on success or the last error on failure.
func PingWithRetry(db *sql.DB, maxAttempts int) error {
	var pingErr error
	for attempt := range maxAttempts {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pingErr = db.PingContext(ctx)
		cancel()
		if pingErr == nil {
			return nil
		}
		if attempt < maxAttempts-1 {
			time.Sleep(time.Second)
		}
	}
	return pingErr
}
