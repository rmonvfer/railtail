// Package main provides railtail HTTP and TCP proxying functionality.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/sync/errgroup"
	"tailscale.com/tsnet"
)

// fwdTCP forwards TCP traffic between the client connection and the Tailscale target.
// It ensures proper resource cleanup and implements timeouts for stability.
func fwdTCP(lstConn net.Conn, ts *tsnet.Server, targetAddr string) error {
	// Always close the local connection when this function exits
	defer lstConn.Close()

	// Create a context with a cancel function for coordinating the copy operations
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure we cancel the context to prevent goroutine leaks

	// Dial the target with a timeout to avoid hanging indefinitely
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	tsConn, err := ts.Dial(dialCtx, "tcp", targetAddr)
	if err != nil {
		return fmt.Errorf("failed to dial tailscale node: %w", err)
	}
	defer tsConn.Close() // Always close the target connection when this function exits

	// Use errgroup to manage the bidirectional copy operations
	g, groupCtx := errgroup.WithContext(ctx)

	// Copy data from local connection to tailscale connection
	g.Go(func() error {
		defer func() {
			// Ensure connections are properly closed after copy completes
			// This helps with half-open connections
			if err := tsConn.SetDeadline(time.Now()); err != nil {
				// This is a best-effort cleanup, so we just continue if it fails
			}
		}()

		if _, err := io.Copy(tsConn, lstConn); err != nil {
			// Cancel context to signal the other goroutine to stop
			cancel()
			return fmt.Errorf("failed to copy data to tailscale node: %w", err)
		}

		// Properly close the write side of the connection to signal EOF
		if conn, ok := tsConn.(interface{ CloseWrite() error }); ok {
			if err := conn.CloseWrite(); err != nil {
				// Log but continue, as we're still closing the entire connection with defer
				return fmt.Errorf("error when closing write end of connection: %w", err)
			}
		}

		return nil
	})

	// Copy data from tailscale connection to local connection
	g.Go(func() error {
		defer func() {
			// Ensure connections are properly closed after copy completes
			if err := lstConn.SetDeadline(time.Now()); err != nil {
				// This is best-effort cleanup, so we just continue if it fails
			}
		}()

		if _, err := io.Copy(lstConn, tsConn); err != nil {
			// Cancel context to signal the other goroutine to stop
			cancel()
			return fmt.Errorf("failed to copy data from tailscale node: %w", err)
		}

		// Properly close the write side of the connection to signal EOF
		if conn, ok := lstConn.(interface{ CloseWrite() error }); ok {
			if err := conn.CloseWrite(); err != nil {
				// Log but continue, as we're still closing the entire connection with defer
				return fmt.Errorf("error when closing write end of connection: %w", err)
			}
		}

		return nil
	})

	// Wait for both copy operations to complete or fail
	if err := g.Wait(); err != nil && groupCtx.Err() == nil {
		return fmt.Errorf("connection error: %w", err)
	}

	return nil
}
