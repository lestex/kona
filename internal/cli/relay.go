package cli

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/lestex/kona/internal/relay"
	"github.com/spf13/cobra"
)

// newRelayCmd is the hidden `kona wg-relay` that kona spawns per CPU node.
func newRelayCmd() *cobra.Command {
	var listen, target, cluster, node string
	cmd := &cobra.Command{
		Use:    "wg-relay",
		Short:  "Relay WireGuard UDP from GPU nodes to a CPU node (started by kona)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			l, err := net.ResolveUDPAddr("udp4", listen)
			if err != nil {
				return err
			}
			t, err := net.ResolveUDPAddr("udp4", target)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			lg := log.New(os.Stderr, fmt.Sprintf("wg-relay %s/%s ", cluster, node), log.LstdFlags)
			lg.Printf("relaying %s -> %s", l, t)
			return (&relay.Relay{Listen: l, Target: t, Logf: lg.Printf}).Run(ctx)
		},
	}
	f := cmd.Flags()
	f.StringVar(&listen, "listen", "", "host address to listen on")
	f.StringVar(&target, "target", "", "CPU node WireGuard address")
	f.StringVar(&cluster, "cluster", "", "cluster name (identifies the process)")
	f.StringVar(&node, "node", "", "CPU node name")
	return cmd
}
