package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeDrainsRequestOnCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	started, release := make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, "finished")
	})}
	t.Cleanup(func() { srv.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, listener, 2*time.Second) }()
	response := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			response <- err.Error()
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		response <- string(body)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("server returned before draining the request: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if body := <-response; body != "finished" {
		t.Fatalf("in-flight request lost: %s", body)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 200*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("listener still accepts connections")
	}
}

func TestServeClosesRequestAfterShutdownDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	started, stopped := make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	})}
	t.Cleanup(func() { srv.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, listener, 20*time.Millisecond) }()
	clientDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://" + listener.Addr().String())
		if res != nil {
			res.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not force close")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("request context was not canceled")
	}
	if err := <-clientDone; err == nil {
		t.Fatal("unfinished response should be interrupted")
	}
}
