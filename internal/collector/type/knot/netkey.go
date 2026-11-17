package knot

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Client networks are stored as a fixed-width key that sorts in address order:
// 32 hex digits of the 16-byte network address (IPv4 as ::ffff:a.b.c.d), a
// slash, and the 3-digit IPv6-form prefix length. A CIDR is then the key range
// [netRange lo, hi), which an index answers without decoding anything.

// Finest client detail the collector sends: /24 for IPv4, /48 for IPv6.
const (
	clientBits4 = 24
	clientBits6 = 48
)

// netKey is the stored key of a masked prefix.
func netKey(p netip.Prefix) string {
	p = p.Masked()
	a16 := p.Addr().As16()
	bits := p.Bits()
	if p.Addr().Is4() {
		bits += 96
	}
	return hex.EncodeToString(a16[:]) + "/" + fmt.Sprintf("%03d", bits)
}

// netKeyOf parses a collector's subnet ("192.0.2.0/24") into its key.
func netKeyOf(subnet string) (string, bool) {
	p, err := netip.ParsePrefix(subnet)
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
	// The last address in the range, plus one; past the top it is "g", which sorts after any hex digit.
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

// netLabel turns a stored key back into a CIDR such as 10.1.2.0/24 or 2001:db8:1::/48.
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

// clientFilter parses a client filter: a CIDR or a bare address. A prefix
// finer than the collector's detail is widened to it; widened reports that.
func clientFilter(raw string) (p netip.Prefix, widened bool, err error) {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "/") {
		p, err = netip.ParsePrefix(raw)
	} else {
		var a netip.Addr
		if a, err = netip.ParseAddr(raw); err == nil {
			p = netip.PrefixFrom(a, a.BitLen())
		}
	}
	if err != nil {
		return netip.Prefix{}, false, err
	}
	if p.Addr().Is4In6() {
		p = netip.PrefixFrom(p.Addr().Unmap(), max(p.Bits()-96, 0))
	}
	p = netip.PrefixFrom(p.Addr().WithZone(""), p.Bits())
	finest := clientBits6
	if p.Addr().Is4() {
		finest = clientBits4
	}
	if p.Bits() > finest {
		p, widened = netip.PrefixFrom(p.Addr(), finest), true
	}
	return p.Masked(), widened, nil
}
