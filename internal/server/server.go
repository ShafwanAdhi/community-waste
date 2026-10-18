package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

func Serve(ctx context.Context, srv *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(listener)
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			srv.Close()
			<-done
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		err := <-done
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
