package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/PeacexF/EnvGraph/internal/server"
)

func newServeCmd() *cobra.Command {
	var (
		flags      scanFlags
		host       string
		port       int
		showValues bool
	)

	cmd := &cobra.Command{
		Use:   "serve [path]",
		Short: "Explore the configuration graph in a browser",
		Long: "Serve opens an interactive view of the configuration graph.\n\n" +
			"The project is re-scanned on every request, so reloading the page\n" +
			"picks up edits without restarting.\n\n" +
			"It listens on localhost only. Values are never sent to the browser\n" +
			"unless --show-values is given, because .env files hold credentials.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := defaultPath(args)

			cfg, err := flags.config(root)
			if err != nil {
				return err
			}

			// Scan once up front so a bad path fails immediately rather than
			// on the first request.
			if _, _, err := flags.run(cmd, root); err != nil {
				return err
			}

			handler := server.New(server.Options{
				Root:       root,
				Scan:       flags.scanOptions(cfg),
				Ignored:    cfg.IgnoresVariable,
				ShowValues: showValues,
			})

			addr := net.JoinHostPort(host, fmt.Sprint(port))
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", addr, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "EnvGraph is serving %s on http://%s\n",
				root, listener.Addr())
			fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl+C to stop.")

			httpServer := &http.Server{
				Handler:           handler,
				ReadHeaderTimeout: 5 * time.Second,
			}

			if err := httpServer.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}

	flags.register(cmd)
	cmd.Flags().StringVar(&host, "host", "127.0.0.1",
		"address to bind (localhost by default; configuration is not public)")
	cmd.Flags().IntVarP(&port, "port", "p", 8080, "port to listen on")
	cmd.Flags().BoolVar(&showValues, "show-values", false,
		"send assigned values to the browser (these are often secrets)")

	return cmd
}
