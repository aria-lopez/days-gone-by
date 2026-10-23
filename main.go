package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aria-lopez/days-gone-by/apeye"
	"github.com/aria-lopez/days-gone-by/bouncer"
	"github.com/aria-lopez/days-gone-by/logger"
)

// isLoopback ;; true if addr only listens on this machine. ":8080" listens on every interface so it is not
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func main() {
	err := dispatch(os.Args[1:])
	if errors.Is(err, errUsage) {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		logger.ErrorF(err, "days-gone-by stopped")
		os.Exit(1)
	}
}

// serve ;; runs the server until ctrl-c or SIGTERM
func serve() error {
	startCtx, cancelStart := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelStart()

	state, err := NewState(startCtx)
	if err != nil {
		return fmt.Errorf("error during setup of application: %w", err)
	}
	defer state.Close()

	// no keys means nobody could ever get in, so dont pretend to be running
	total, _, err := state.squeal.CountActiveKeys(startCtx)
	if err != nil {
		return fmt.Errorf("couldnt check for api keys: %w", err)
	}
	if total == 0 {
		return errors.New("there are no api keys yet, run `days-gone-by init` first")
	}

	logger.InfoF(" running addr [%s] path [%s] api keys [%d]", state.cfg.ServerAddr, state.defaultPath, total)
	if !isLoopback(state.cfg.ServerAddr) {
		logger.InfoF("WARNING [%s] is reachable from other machines and keys travel in a header, put TLS (a reverse proxy) in front of it", state.cfg.ServerAddr)
	}

	api := apeye.NewAPI(state.store, state.squeal, bouncer.New(state.squeal), state.cfg)
	srv := &http.Server{
		Addr:              state.cfg.ServerAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute, // between requests on a kept-alive connection, unset means for ever
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// housekeeping runs in the background until we shut down
	maintainCtx, cancelMaintain := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { api.Maintain(maintainCtx) })
	// defers run backwards: cancel, then wait for the sweep to stop, and only then state.Close()
	// closes the db. the cancel has to be there for the early return below or the wait never ends
	defer wg.Wait()
	defer cancelMaintain()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err // server never started
	case <-ctx.Done():
	}

	// make sure to handle in-flight during shutdown
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()

	return srv.Shutdown(shutdownCtx)
}
