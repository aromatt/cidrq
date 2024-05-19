package pkg

import (
	"bufio"
	//"fmt"
	"io"
	"net/netip"
)

type CidrProcessor struct {
	ParseFn   func(string) (netip.Prefix, error)
	HandlerFn func(netip.Prefix, string) error
	ErrFn     func(error) error
}

func (p *CidrProcessor) Process(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		prefix, err := p.ParseFn(line)
		if err != nil {
			if err = p.ErrFn(err); err != nil {
				return err
			}
		} else {
			if err = p.HandlerFn(prefix, line); err != nil {
				if err = p.ErrFn(err); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
