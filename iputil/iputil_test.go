package iputil

import (
	"math/big"
	"net/netip"
	"strings"
	"testing"
)

func TestLookupPortRefusesNonPublic(t *testing.T) {
	var tests = []struct {
		in string
	}{
		{"127.0.0.1"},
		{"10.0.0.1"},
		{"169.254.169.254"},
		{"::1"},
		{"fe80::1"},
		{"0.0.0.0"},
	}
	for _, tt := range tests {
		addr, err := netip.ParseAddr(tt.in)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", tt.in, err)
		}
		err = LookupPort(addr, 80)
		if err == nil {
			t.Errorf("expected error for %s, got nil", tt.in)
			continue
		}
		if !strings.Contains(err.Error(), "refusing to dial non-public address") {
			t.Errorf("unexpected error for %s: %v", tt.in, err)
		}
	}
}

func TestToDecimal(t *testing.T) {
	var msb = new(big.Int)
	msb, _ = msb.SetString("80000000000000000000000000000000", 16)

	var tests = []struct {
		in  string
		out *big.Int
	}{
		{"127.0.0.1", big.NewInt(2130706433)},
		{"::1", big.NewInt(1)},
		{"8000::", msb},
	}
	for _, tt := range tests {
		addr, _ := netip.ParseAddr(tt.in)
		i := ToDecimal(addr)
		if tt.out.Cmp(i) != 0 {
			t.Errorf("Expected %d, got %d for IP %s", tt.out, i, tt.in)
		}
	}
}
