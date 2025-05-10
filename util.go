package main

import (
	"cmp"
	"net"
	"net/netip"
	"net/url"
	"os"

	"github.com/aromatt/netipds"
)

// https://github.com/golang/go/issues/61642
func PrefixCompare(p1, p2 netip.Prefix) int {
	if c := cmp.Compare(p1.Addr().BitLen(), p2.Addr().BitLen()); c != 0 {
		return c
	}
	if c := cmp.Compare(p1.Bits(), p2.Bits()); c != 0 {
		return c
	}
	return p1.Addr().Compare(p2.Addr())
}

// If the string does end with an explicit subnet mask, then append '/32'
func EnsurePrefix(s string) string {
	isIpv6 := false
	for _, c := range s {
		if c == '/' {
			return s
		}
		if c == ':' {
			isIpv6 = true
		}
	}
	if isIpv6 {
		return s + "/128"
	}
	return s + "/32"
}

// StringMaybeAddr returns the Prefix as a string, stripping the prefix length
// if the Prefix is a single IP.
func StringMaybeAddr(p netip.Prefix) string {
	if p.Bits() == p.Addr().BitLen() {
		return p.Addr().String()
	}
	return p.String()
}

// StrSliceToPrefixSlice converts a slice of CIDR strings to a slice of Prefixes.
func StrSliceToPrefixSlice(cidrStrs []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, len(cidrStrs))
	for i, cidrStr := range cidrStrs {
		p, err := netip.ParsePrefix(EnsurePrefix(cidrStr))
		if err != nil {
			return nil, err
		}
		prefixes[i] = p
	}
	return prefixes, nil
}

// ToSliceOfOneFn converts a function that returns a single value and an error
// into a function that returns a slice of one value and an error.
func ToSliceOfOneFn[A, B any](fn func(A) (B, error)) func(A) ([]B, error) {
	return func(a A) ([]B, error) {
		b, err := fn(a)
		if err != nil {
			return nil, err
		}
		return []B{b}, nil
	}
}

// ParsePrefixOrAddr parses a string as a CIDR prefix or an IP address.
func ParsePrefixOrAddr(s string) (netip.Prefix, error) {
	return netip.ParsePrefix(EnsurePrefix(s))
}

// ParseHost parses a string as an IP with an optional :port suffix and returns
// the IP as a netip.Prefix.
func ParseHost(s string) (netip.Prefix, error) {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		host = s
	}
	return netip.ParsePrefix(EnsurePrefix(host))
}

// ParseUrl parses a string as a URL and returns the host as a netip.Prefix.
// If the URL is invalid, an error is returned.
// If the host is not an IP address, an error is returned.
func ParseUrl(s string) (netip.Prefix, error) {
	u, err := url.Parse(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.ParsePrefix(EnsurePrefix(u.Hostname()))
}

func ValParser(acceptUrl bool, acceptHostPort bool) func(string) (netip.Prefix, error) {
	return func(s string) (netip.Prefix, error) {
		var err error
		var p netip.Prefix
		if acceptUrl {
			p, err = ParseUrl(s)
			if err == nil {
				return p, nil
			}
		}
		if acceptHostPort {
			p, err = ParseHost(s)
			if err == nil {
				return p, nil
			}
		}
		return ParsePrefixOrAddr(s)
	}
}

func LoadPrefixSetBuilderFromFile(
	path string,
	errFn func(string, error) error,
) (*netipds.PrefixSetBuilder, error) {
	psb := netipds.PrefixSetBuilder{}

	p := CidrProcessor{
		ValParser: ParsePrefixOrAddr,
		HandlerFn: func(parsed *ParsedLine) error {
			for _, prefix := range parsed.Prefixes {
				psb.Add(prefix)
			}
			return nil
		},
		ErrFn: errFn,
	}

	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	err = p.Process(r)
	if err != nil {
		return nil, err
	}
	return &psb, nil
}

func LoadPrefixSetFromFile(
	path string,
	errFn func(string, error) error,
) (*netipds.PrefixSet, error) {
	psb, err := LoadPrefixSetBuilderFromFile(path, errFn)
	if err != nil {
		return nil, err
	}
	return psb.PrefixSet(), nil
}
