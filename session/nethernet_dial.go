package session

import (
	"context"
	"fmt"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/paroxity/portal/server"
)

// netherNetDialAddress derives the doubled-port form ("scheme://host:port:port") gophertunnel's login
// validation requires in ClientData.ServerAddress for a NetherNet connection. gophertunnel sets
// ServerAddress to the exact string used to dial, so it can't simultaneously be that form and the clean
// URL the transport itself needs -- netherNetDialSignaling reconciles the two.
func netherNetDialAddress(signalingURL string) (string, error) {
	u, err := server.ParseNetherNetAddress(signalingURL)
	if err != nil {
		return "", fmt.Errorf("nethernet address %w", err)
	}
	return signalingURL + ":" + u.Port(), nil
}

// netherNetDialSignaling wraps an *endpoint.Client so a dial can use the doubled-port address as its
// nethernet.Dialer networkID (satisfying ClientData.ServerAddress validation) while still reaching the
// backend at its real, clean signaling URL: it rewrites NetworkID from dirty to clean on outgoing signals,
// and back again on incoming ones, since the dialer correlates signals by exact NetworkID match.
type netherNetDialSignaling struct {
	*endpoint.Client
	dirty string // doubled-port address, used as the nethernet.Dialer networkID
	clean string // the signaling endpoint's real URL
}

func newNetherNetDialSignaling(clean string) (*netherNetDialSignaling, error) {
	dirty, err := netherNetDialAddress(clean)
	if err != nil {
		return nil, err
	}
	return &netherNetDialSignaling{Client: endpoint.NewClient(), dirty: dirty, clean: clean}, nil
}

func (s *netherNetDialSignaling) Signal(ctx context.Context, signal *nethernet.Signal) error {
	rewritten := *signal
	rewritten.NetworkID = s.clean
	return s.Client.Signal(ctx, &rewritten)
}

func (s *netherNetDialSignaling) Notify(n nethernet.Notifier) (stop func()) {
	return s.Client.Notify(netherNetNotifierFunc(func(signal *nethernet.Signal) bool {
		return n.NotifySignal(rewriteNetherNetSignal(signal, s.clean, s.dirty))
	}))
}

// rewriteNetherNetSignal returns signal unchanged unless its NetworkID equals from, in which case it
// returns a shallow copy with NetworkID replaced by to.
func rewriteNetherNetSignal(signal *nethernet.Signal, from, to string) *nethernet.Signal {
	if signal.NetworkID != from {
		return signal
	}
	rewritten := *signal
	rewritten.NetworkID = to
	return &rewritten
}

// netherNetNotifierFunc adapts a function to nethernet.Notifier.
type netherNetNotifierFunc func(signal *nethernet.Signal) bool

func (f netherNetNotifierFunc) NotifySignal(signal *nethernet.Signal) bool { return f(signal) }
