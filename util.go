package main

import (
	"cmp"
	"fmt"
	"io"
	"net/netip"
	"os"

	"go4.org/netipx"
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

func BytesToUint32(b [4]byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func Uint32ToBytes(u uint32) [4]byte {
	return [4]byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
}

// if the string does end with an explicit subnet mask, then append '/32'
func EnsurePrefix(s string) string {
	if s[len(s)-2] != '/' && s[len(s)-3] != '/' {
		return s + "/32"
	}
	return s
}

func CidrStrSliceToPrefixSlice(cidrStrs []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, len(cidrStrs))
	for i, cidrStr := range cidrStrs {
		prefixes[i] = netip.MustParsePrefix(EnsurePrefix(cidrStr))
	}
	return prefixes
}

func ReadCidrs(r io.Reader, fn func(netip.Prefix) error) error {
	for {
		var cidrStr string
		if _, err := fmt.Fscanln(r, &cidrStr); err != nil {
			if err == io.EOF {
				break
			} else {
				return err
			}
		}
		prefix := netip.MustParsePrefix(EnsurePrefix(cidrStr))
		if err := fn(prefix); err != nil {
			return err
		}
	}
	return nil
}

func ReadCidrsFromFile(path string, fn func(netip.Prefix) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return ReadCidrs(file, fn)
}

func LoadIPSetBuilderFromFile(path string) (netipx.IPSetBuilder, error) {
	ipsb := netipx.IPSetBuilder{}
	if err := ReadCidrsFromFile(path, func(prefix netip.Prefix) error {
		ipsb.AddPrefix(prefix)
		return nil
	}); err != nil {
		return netipx.IPSetBuilder{}, err
	}
	return ipsb, nil
}

func LoadIPSetFromFile(path string) (*netipx.IPSet, error) {
	ipsb, err := LoadIPSetBuilderFromFile(path)
	if err != nil {
		return nil, err
	}
	return ipsb.IPSet()
}
