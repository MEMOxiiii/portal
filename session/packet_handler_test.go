package session

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestClearLegacyIdentity(t *testing.T) {
	tests := []struct {
		name       string
		legacyAuth bool
		wantXUID   string
	}{
		{name: "modern backend preserves identity", legacyAuth: false, wantXUID: "12345"},
		{name: "legacy backend clears identity", legacyAuth: true, wantXUID: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chat := &packet.Text{XUID: "12345"}
			book := &packet.BookEdit{XUID: "12345"}
			clearLegacyIdentity(chat, test.legacyAuth)
			clearLegacyIdentity(book, test.legacyAuth)
			if chat.XUID != test.wantXUID || book.XUID != test.wantXUID {
				t.Fatalf("identity fields = (%q, %q), want %q", chat.XUID, book.XUID, test.wantXUID)
			}
		})
	}
}
