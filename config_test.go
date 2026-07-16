package portal

import (
	"encoding/json"
	"testing"
)

func TestDefaultFlushRate(t *testing.T) {
	if got := DefaultConfig().Network.FlushRateMS; got != 20 {
		t.Fatalf("default flush rate = %dms, want 20ms", got)
	}
}

func TestFlushRateConfigCompatibility(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int
	}{
		{name: "field omitted", data: `{"network":{"address":":19132"}}`, want: 20},
		{name: "gophertunnel default", data: `{"network":{"flush_rate_ms":0}}`, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			if err := json.Unmarshal([]byte(test.data), &config); err != nil {
				t.Fatal(err)
			}
			if got := config.Network.FlushRateMS; got != test.want {
				t.Fatalf("flush rate = %dms, want %dms", got, test.want)
			}
		})
	}
}
