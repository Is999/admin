package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestLimitHTTPDrainClosesLongRequest 确保长请求不会阻塞后续资源关闭。
func TestLimitHTTPDrainClosesLongRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	requestStarted := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	})}
	drainDone := make(chan struct{})
	defer close(drainDone)
	limitHTTPDrain(server, 30*time.Millisecond, drainDone)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()
	go func() {
		_, _ = http.Get("http://" + listener.Addr().String())
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("long request did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- server.Shutdown(context.Background())
	}()
	select {
	case err = <-shutdownDone:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shutdown exceeded HTTP drain limit")
	}
	if err = <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve error = %v", err)
	}
}
