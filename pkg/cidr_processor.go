package pkg

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"strings"
)

// CidrProcessor consolidates all of the configuration provided by a user for
// processing lists of CIDRs.
type CidrProcessor struct {
	Fields    []int
	Delimiter string
	ValParser func(string) (netip.Prefix, error)
	HandlerFn func(*ParsedLine) error
	ErrFn     func(string, error) error
}

// LineParser returns a function that parses a line into a slice of prefixes.
func LineParser(
	fields []int,
	delimiter string,
	valParser func(string) (netip.Prefix, error),
) func(string) ([]netip.Prefix, error) {
	if len(fields) != 0 {
		if delimiter == "\\t" {
			delimiter = "\t"
		}
		return func(line string) ([]netip.Prefix, error) {
			// Split line into fields
			parts := strings.Split(line, delimiter)
			prefixes := []netip.Prefix{}
			var prefix netip.Prefix
			var err error
			for _, f := range fields {
				if f > len(parts) {
					return prefixes, fmt.Errorf("Field %d not found in line: %s", f, line)
				}
				prefix, err = valParser(parts[f-1])
				if err != nil {
					return prefixes, err
				}
				prefixes = append(prefixes, prefix)
			}
			return prefixes, nil
		}
	}
	return ToSliceOfOneFn(valParser)
}

type ParsedLine struct {
	Raw       string
	parts     []string
	Prefixes  []netip.Prefix
	fields    []bool
	delimiter string
}

// Clean returns the raw line with any parsed fields replaced by their
// extracted Prefixes.
func (p *ParsedLine) Clean() string {
	if p.delimiter == "" {
		return StringMaybeAddr(p.Prefixes[0])
	}
	b := strings.Builder{}
	for i, part := range p.parts {
		if p.fields[i] {
			b.WriteString(StringMaybeAddr(p.Prefixes[i]))
		} else {
			b.WriteString(part)
		}
		if i < len(p.parts)-1 {
			b.WriteString(p.delimiter)
		}
	}
	return b.String()
}

func (p *CidrProcessor) parseLine(line string) (*ParsedLine, error) {
	parsed := ParsedLine{Raw: line}
	if len(p.Fields) > 0 {
		if p.Delimiter == "\\t" {
			p.Delimiter = "\t"
		}
		parsed.delimiter = p.Delimiter
		parsed.parts = strings.Split(line, p.Delimiter)
		parsed.fields = make([]bool, len(parsed.parts))
		var prefix netip.Prefix
		var err error
		for _, f := range p.Fields {
			if f > len(parsed.parts) {
				return nil, fmt.Errorf("Field %d not found in line: %s", f, line)
			}
			prefix, err = p.ValParser(parsed.parts[f-1])
			if err != nil {
				return nil, err
			}
			parsed.Prefixes = append(parsed.Prefixes, prefix)
			parsed.fields[f-1] = true
		}
		return &parsed, nil
	} else {
		prefix, err := p.ValParser(line)
		if err != nil {
			return nil, err
		}
		parsed.Prefixes = append(parsed.Prefixes, prefix)
		return &parsed, nil
	}
}

// Process parses all lines from the provided reader and calls p.HandlerFn on
// each parsed line, handling errors with p.ErrFn.
func (p *CidrProcessor) Process(r io.Reader) error {
	scanner := bufio.NewScanner(r)

	numLines := 0

	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		numLines++
		parsedLine, err := p.parseLine(line)
		if err != nil {
			if err = p.ErrFn(line, err); err != nil {
				return err
			}
		} else {
			if err = p.HandlerFn(parsedLine); err != nil {
				if err = p.ErrFn(line, err); err != nil {
					return err
				}
			}
		}
	}

	Logf("Processed %d lines\n", numLines)
	return scanner.Err()
}
