package server

import "testing"

func TestParseNetherNetAddress(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantPort string
		wantErr  bool
	}{
		{name: "http with port", in: "http://127.0.0.1:19135", wantPort: "19135"},
		{name: "https with port", in: "https://example.com:19133", wantPort: "19133"},
		{name: "no port", in: "http://127.0.0.1", wantErr: true},
		{name: "not a url", in: "127.0.0.1:19135", wantErr: true},
		{name: "non-http scheme", in: "ftp://127.0.0.1:19135", wantErr: true},
		{name: "has a path", in: "http://127.0.0.1:19135/nethernet", wantErr: true},
		{name: "trailing slash path", in: "http://127.0.0.1:19135/", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u, err := ParseNetherNetAddress(test.in)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseNetherNetAddress(%q) error = nil, want error", test.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseNetherNetAddress(%q) error = %v, want nil", test.in, err)
			}
			if got := u.Port(); got != test.wantPort {
				t.Fatalf("ParseNetherNetAddress(%q).Port() = %q, want %q", test.in, got, test.wantPort)
			}
		})
	}
}
