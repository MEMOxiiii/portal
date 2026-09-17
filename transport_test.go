package portal

import "testing"

func TestParseNetherNetPortRange(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    netherNetPortRange
		wantErr bool
	}{
		{name: "empty", in: "", want: netherNetPortRange{}},
		{name: "single port", in: "19133", want: netherNetPortRange{Min: 19133, Max: 19133}},
		{name: "range", in: "19133-19140", want: netherNetPortRange{Min: 19133, Max: 19140}},
		{name: "inverted range", in: "19140-19133", wantErr: true},
		{name: "not a number", in: "abc", wantErr: true},
		{name: "malformed range", in: "19133-abc", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseNetherNetPortRange(test.in)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseNetherNetPortRange(%q) error = nil, want error", test.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseNetherNetPortRange(%q) error = %v, want nil", test.in, err)
			}
			if got != test.want {
				t.Fatalf("parseNetherNetPortRange(%q) = %+v, want %+v", test.in, got, test.want)
			}
		})
	}
}
