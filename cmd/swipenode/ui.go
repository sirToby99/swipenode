package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirToby99/swipenode/internal/controlplane"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newUICommand(), newEvidenceCommand(), newVerificationCommand())
}

func newUICommand() *cobra.Command {
	var listenAddress string
	command := &cobra.Command{
		Use:   "ui",
		Short: "Run the customer-local SwipeNode Control Plane",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validateUIListenAddress(listenAddress); err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			service, err := controlplane.Open(command.Context(), cwd)
			if err != nil {
				return fmt.Errorf("open customer Control Plane: %w", err)
			}
			handler, err := controlplane.NewHandler(service)
			if err != nil {
				return err
			}
			listener, err := net.Listen("tcp", listenAddress)
			if err != nil {
				return fmt.Errorf("listen for customer Control Plane: %w", err)
			}
			defer listener.Close()
			server := &http.Server{
				Handler:           handler,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       15 * time.Second,
				WriteTimeout:      60 * time.Second,
				IdleTimeout:       60 * time.Second,
				MaxHeaderBytes:    32 << 10,
			}
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(stop)
			go func() {
				select {
				case <-stop:
				case <-command.Context().Done():
				}
				shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdownContext)
			}()
			_, _ = fmt.Fprintf(command.OutOrStdout(), "SwipeNode Customer Control Plane listening on http://%s\n", listener.Addr())
			err = server.Serve(listener)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve customer Control Plane: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&listenAddress, "listen", "127.0.0.1:8082", "loopback listen address")
	return command
}

func validateUIListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf("--listen must be an explicit loopback host:port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("customer Control Plane permits loopback binding only")
	}
	return nil
}
