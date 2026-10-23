package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aria-lopez/days-gone-by/apeye"
	"github.com/aria-lopez/days-gone-by/logger"
)

func main() {
	if err := run(); err != nil {
		logger.ErrorF(err, "days-gone-by stopped")
		os.Exit(1)
	}
}

func run() error {
	startCtx, cancelStart := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelStart()

	state, err := NewState(startCtx)
	if err != nil {
		return fmt.Errorf("error during setup of application: %w", err)
	}
	defer state.Close()
	logger.InfoF(" running addr [%s] path [%s]", state.serverAddr, state.defaultPath)

	api := apeye.NewAPI(state.store, state.squel, int(state.maxUpload))
	srv := &http.Server{
		Addr:              state.serverAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
