package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/proxy"
)

func init() {
	addProxyCommand(rootCmd)
}

// addProxyCommand adds the proxy command to a parent command.
func addProxyCommand(parent *cobra.Command) {
	proxyCmd := &cobra.Command{
		Use:   "proxy <sandbox_id> [local_port:]<remote_port>",
		Short: "Forward a sandbox port to localhost",
		Long: `Forward a remote sandbox port to a local address, similar to kubectl port-forward.

This creates a local HTTP/WebSocket reverse proxy that forwards all requests to
the specified port on the remote sandbox. Access tokens are automatically injected
into all proxied requests.

Both HTTP and WebSocket protocols are fully supported, making this suitable for
web development servers (e.g., Vite, webpack-dev-server), API servers, and any
HTTP-based service running in the sandbox.

NOTE: The remote port must be explicitly opened in the AGS sandbox console before
use. Navigate to your sandbox instance -> Network -> Open Port, and add the remote
port number to the allowlist. Requests to ports that are not configured will be
rejected by the gateway.

Port Syntax:
  <remote_port>                Forward remote port to the same local port
  <local_port>:<remote_port>   Forward remote port to a specific local port

Examples:
  # Forward sandbox port 8080 to localhost:8080
  ags proxy sandbox-xxx 8080

  # Forward sandbox port 8080 to localhost:3000
  ags proxy sandbox-xxx 3000:8080

  # Forward with explicit address
  ags proxy sandbox-xxx 3000:8080 --address 0.0.0.0`,
		Args: cobra.ExactArgs(2),
		RunE: runProxy,
	}

	proxyCmd.Flags().String("address", "127.0.0.1", "Local address to bind to")
	proxyCmd.Flags().Bool("verbose", false, "Enable verbose request logging")

	parent.AddCommand(proxyCmd)
}

// parsePortSpec parses a port specification in the format [local_port:]<remote_port>.
// Returns (localPort, remotePort, error).
func parsePortSpec(spec string) (int, int, error) {
	parts := strings.SplitN(spec, ":", 2)

	if len(parts) == 1 {
		// Just remote_port, use same port for local
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid port: %q", parts[0])
		}
		if port <= 0 || port > 65535 {
			return 0, 0, fmt.Errorf("port must be between 1 and 65535, got %d", port)
		}
		return port, port, nil
	}

	// local_port:remote_port
	localPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid local port: %q", parts[0])
	}
	if localPort <= 0 || localPort > 65535 {
		return 0, 0, fmt.Errorf("local port must be between 1 and 65535, got %d", localPort)
	}

	remotePort, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid remote port: %q", parts[1])
	}
	if remotePort <= 0 || remotePort > 65535 {
		return 0, 0, fmt.Errorf("remote port must be between 1 and 65535, got %d", remotePort)
	}

	return localPort, remotePort, nil
}

func runProxy(cmd *cobra.Command, args []string) error {
	sandboxID := args[0]
	portSpec := args[1]

	if err := config.Validate(); err != nil {
		return err
	}

	localPort, remotePort, err := parsePortSpec(portSpec)
	if err != nil {
		return fmt.Errorf("invalid port specification: %w", err)
	}

	address, err := cmd.Flags().GetString("address")
	if err != nil {
		return fmt.Errorf("failed to get address flag: %w", err)
	}

	// Validate the address format before attempting to bind.
	// "localhost" is a special hostname, all other values must be a valid IP.
	if address != "localhost" {
		if net.ParseIP(address) == nil {
			return fmt.Errorf("invalid address %q: must be a valid IP address or \"localhost\"", address)
		}
	}

	// Warn when binding to a non-loopback address: the proxy automatically
	// injects the sandbox access token, so any host that can reach this
	// address will have full access to the sandbox service.
	// Loopback addresses: 127.0.0.1 (IPv4), ::1 (IPv6), localhost (hostname).
	if address != "127.0.0.1" && address != "localhost" && address != "::1" {
		fmt.Fprintf(os.Stderr, "Warning: binding to %s exposes the proxy (and the sandbox access token) to the network.\n", address)
	}
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("failed to get verbose flag: %w", err)
	}

	// Acquire a token once upfront. The access token's lifetime is bound to the
	// sandbox instance lifecycle — it remains valid as long as the sandbox is
	// running and becomes invalid only when the sandbox is destroyed. There is
	// no need to refresh the token or handle 401 responses during the proxy's
	// lifetime.
	token, err := acquireInstanceToken(context.Background(), sandboxID)
	if err != nil {
		return fmt.Errorf("failed to acquire access token: %w", err)
	}

	cfg := config.Get()
	domain := cfg.DataPlaneRegionDomain()

	// Use net.JoinHostPort so IPv6 addresses are correctly bracketed,
	// e.g. "::1" → "[::1]:8080" rather than the invalid "::1:8080".
	listenAddr := net.JoinHostPort(address, strconv.Itoa(localPort))

	p, err := proxy.New(proxy.Options{
		InstanceID:    sandboxID,
		Domain:        domain,
		RemotePort:    remotePort,
		Token:         token,
		ListenAddress: listenAddr,
		Insecure:      false,
		Verbose:       verbose,
	})
	if err != nil {
		return fmt.Errorf("failed to create proxy: %w", err)
	}

	addr, err := p.Start()
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}

	fmt.Printf("Forwarding from %s -> %d\n", addr, remotePort)
	fmt.Printf("  Local:  http://%s\n", addr)
	fmt.Printf("  Remote: https://%d-%s.%s\n", remotePort, sandboxID, domain)
	fmt.Println("\nPress Ctrl+C to stop.")

	// Block until SIGINT/SIGTERM, then gracefully shut down.
	// p.Stop() internally waits up to 5 s for in-flight requests to finish.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	fmt.Println("\nStopping proxy...")
	p.Stop()

	return nil
}
