package pkg

import (
	"bufio"
	//"fmt"
	"io"
	"net/netip"
)

type CidrProcessor struct {
	LineParser func(string) ([]netip.Prefix, error)
	HandlerFn  func([]netip.Prefix, string) error
	ErrFn      func(string, error) error
}

func (p *CidrProcessor) Process(r io.Reader) error {
	scanner := bufio.NewScanner(r)

	numLines := 0

	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		numLines++
		prefixes, err := p.LineParser(line)
		if err != nil {
			if err = p.ErrFn(line, err); err != nil {
				return err
			}
		} else {
			if err = p.HandlerFn(prefixes, line); err != nil {
				if err = p.ErrFn(line, err); err != nil {
					return err
				}
			}
		}
	}

	Logf("Processed %d lines\n", numLines)
	return scanner.Err()
}
