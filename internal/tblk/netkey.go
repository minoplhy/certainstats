package tblk

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Networks are stored as a fixed-width key that sorts in address order: 32
// hex digits of the 16-byte network address (IPv4 as ::ffff:a.b.c.d), a slash
// and the 3-digit IPv6-form prefix length. A CIDR filter is then the key range
// [netRange lo, hi). Same encoding as knot/netkey.go.

func netKey(p netip.Prefix) string {
	p = p.Masked()
	a16 := p.Addr().As16()
	bits := p.Bits()
	if p.Addr().Is4() {
		bits += 96
	}
	return hex.EncodeToString(a16[:]) + "/" + fmt.Sprintf("%03d", bits)
}

// netKeyOf parses a CIDR ("192.0.2.0/24") into its stored key. Other, the
// folded network, is stored as it is: it sorts before every hex key, so no
// CIDR range matches it.
func netKeyOf(cidr string) (string, bool) {
	if cidr == Other {
		return Other, true
	}
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", false
	}
	return netKey(p), true
}

// netRange is the key range [lo, hi) of every stored network inside cidr.
func netRange(cidr netip.Prefix) (lo, hi string) {
	cidr = cidr.Masked()
	a := cidr.Addr().As16()
	bits := cidr.Bits()
	if cidr.Addr().Is4() {
		bits += 96
	}
	lo = hex.EncodeToString(a[:])
	last := a
	for i := bits; i < 128; i++ {
		last[i/8] |= 1 << (7 - uint(i%8))
	}
	for i := 15; i >= 0; i-- {
		last[i]++
		if last[i] != 0 {
			return lo, hex.EncodeToString(last[:])
		}
	}
	return lo, "g"
}

// netLabel turns a stored key back into a CIDR.
func netLabel(key string) string {
	addrHex, bitsRaw, ok := strings.Cut(key, "/")
	raw, err := hex.DecodeString(addrHex)
	bits, berr := strconv.Atoi(bitsRaw)
	if !ok || err != nil || berr != nil || len(raw) != 16 {
		return key
	}
	addr := netip.AddrFrom16([16]byte(raw))
	if addr.Is4In6() {
		return netip.PrefixFrom(addr.Unmap(), bits-96).String()
	}
	return netip.PrefixFrom(addr, bits).String()
}
