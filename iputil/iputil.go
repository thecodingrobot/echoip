package iputil

import (
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"time"
)

func LookupAddr(ip netip.Addr) (string, error) {
	names, err := net.LookupAddr(ip.String())
	if err != nil || len(names) == 0 {
		return "", err
	}
	// Always return unrooted name
	return strings.TrimRight(names[0], "."), nil
}

func LookupPort(ip netip.Addr, port uint64) error {
	address := fmt.Sprintf("[%s]:%d", ip, port)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}

func ToDecimal(ip netip.Addr) *big.Int {
	i := big.NewInt(0)
	i.SetBytes(ip.AsSlice())
	return i
}
