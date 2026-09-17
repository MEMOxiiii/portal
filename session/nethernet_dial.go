package session

import (
	"context"
	"fmt"
	"net/url"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
)

// netherNetDialAddress derives the address gophertunnel's login validation requires in
// login.ClientData.ServerAddress for a NetherNet connection, from signalingURL, the clean
// "scheme://host:port" URL of a backend server's signaling endpoint.
//
// gophertunnel's ClientData validation (minecraft/protocol/login/data.go) expects a NetherNet
// ServerAddress in the literal form "scheme://host:port:port" -- the port repeated a second time after
// the URL -- and rejects a plain URL as invalid. Since gophertunnel's Dialer always sets ServerAddress to
// the exact address string used to dial (see minecraft.Dialer.DialContextNetwork), that string can't
// simultaneously be a plain URL (which is what the NetherNet transport itself needs to actually connect)
// and this doubled-port form. netherNetDialSignaling resolves the conflict: the doubled-port address is
// what gets passed to Dial, and netherNetDialSignaling translates it back to the clean URL before it
// reaches the network.
func netherNetDialAddress(signalingURL string) (string, error) {
	u, err := url.Parse(signalingURL)
	if err != nil {
		return "", fmt.Errorf("parse nethernet address: %w", err)
	}
	port := u.Port()
	if port == "" {
		return "", fmt.Errorf("nethernet address %q has no port", signalingURL)
	}
	return signalingURL + ":" + port, nil
}

// netherNetDialSignaling adapts an *endpoint.Client so a dial can use netherNetDialAddress's doubled-port
// form as its nethernet.Dialer networkID -- to satisfy gophertunnel's ClientData.ServerAddress validation
// -- while still reaching the backend at its real, clean signaling URL.
//
// nethernet.Dialer correlates every signal it sends and receives by comparing NetworkID strings against
// the networkID it was given, so simply handing endpoint.Client the doubled-port string as its own target
// URL isn't an option either: it would try to dial that malformed address directly. Instead,
// netherNetDialSignaling sits between the two, rewriting NetworkID from the doubled-port form to the clean
// URL on outgoing signals (so endpoint.Client's HTTP requests reach the real server) and back again on
// incoming ones (so nethernet.Dialer's own correlation, which still expects the doubled-port form, keeps
// matching).
type netherNetDialSignaling struct {
	*endpoint.Client
	// dirty is the doubled-port address passed to nethernet.Dialer.DialContext as its networkID.
	dirty string
	// clean is the signaling endpoint's real URL, used for the underlying HTTP requests.
	clean string
}

// Signal rewrites signal.NetworkID from dirty to clean before delegating to the underlying endpoint.Client,
// so the HTTP request it builds targets the server's real signaling URL rather than the doubled-port form.
func (s *netherNetDialSignaling) Signal(ctx context.Context, signal *nethernet.Signal) error {
	rewritten := *signal
	rewritten.NetworkID = s.clean
	return s.Client.Signal(ctx, &rewritten)
}

// Notify rewrites the NetworkID of every signal from clean back to dirty before forwarding it to n, so
// nethernet.Dialer's own correlation of incoming signals -- which expects dirty, the value it was given as
// networkID -- keeps matching.
func (s *netherNetDialSignaling) Notify(n nethernet.Notifier) (stop func()) {
	return s.Client.Notify(netherNetRewritingNotifier{Notifier: n, from: s.clean, to: s.dirty})
}

// netherNetRewritingNotifier rewrites the NetworkID of a signal matching "from" to "to" before forwarding
// it to the wrapped Notifier, leaving any signal with a different NetworkID untouched.
type netherNetRewritingNotifier struct {
	nethernet.Notifier
	from, to string
}

func (n netherNetRewritingNotifier) NotifySignal(signal *nethernet.Signal) bool {
	if signal.NetworkID != n.from {
		return n.Notifier.NotifySignal(signal)
	}
	rewritten := *signal
	rewritten.NetworkID = n.to
	return n.Notifier.NotifySignal(&rewritten)
}
