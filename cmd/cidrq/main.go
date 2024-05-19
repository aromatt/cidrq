package main

import (
	//"flag"
	"fmt"
	cli "github.com/urfave/cli/v2"
	"go4.org/netipx"
	"io"
	"net/netip"
	"os"
	"strings"

	cq "github.com/aromatt/cidrq/pkg"
	//profile "github.com/pkg/profile"
	"github.com/aromatt/netipmap"
)

const (
	AbortOnError = "abort"
	WarnOnError  = "warn"
	SkipOnError  = "skip"
)

var errorHandler func(error) error

func setErrorHandler(c *cli.Context) error {
	v := c.String("err")
	switch v {
	case AbortOnError:
		errorHandler = func(err error) error { return err }
	case WarnOnError:
		errorHandler = func(err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			}
			return nil
		}
	case SkipOnError:
		errorHandler = func(err error) error { return nil }
	default:
		return fmt.Errorf("Invalid error action %s", v)
	}
	return nil
}

func validatePath(c *cli.Context, v string) error {
	if _, err := os.Stat(v); os.IsNotExist(err) {
		return fmt.Errorf("File %s does not exist", v)
	}
	return nil
}

// Iterate over CIDRs from stdin and files (args), calling fn for each
func iterPathArgs(c *cli.Context, fn func(io.Reader) error) error {
	var err error

	// Assume stdin if no args
	if c.NArg() == 0 {
		if err = fn(io.Reader(os.Stdin)); err != nil {
			return err
		}
	}

	// Iterate over files ('-' means stdin, which only works once)
	for _, path := range c.Args().Slice() {
		didReadStdin := false
		if path == "-" && !didReadStdin {
			didReadStdin = true
			if err = fn(io.Reader(os.Stdin)); err != nil {
				return err
			}
		} else {
			r, err := os.Open(path)
			if err != nil {
				return err
			}
			if err = fn(r); err != nil {
				return err
			}
		}
	}

	return nil
}

func handleMerge(c *cli.Context) error {
	merged := netipmap.PrefixSetBuilder{}

	// Set up processor
	p := cq.CidrProcessor{
		ParseFn: cq.ParsePrefixOrAddr,
		HandlerFn: func(prefix netip.Prefix, line string) error {
			merged.Add(prefix)
			return nil
		},
		ErrFn: errorHandler,
	}

	//fmt.Println(merged.String())

	// Process all inputs
	err := iterPathArgs(c, func(r io.Reader) error {
		return p.Process(r)
	})
	if err != nil {
		return err
	}

	// Output merged CIDRs
	mergedIPs := merged.PrefixSet()
	for _, p := range mergedIPs.Prefixes() {
		fmt.Println(p)
	}
	return nil
}

func parseLine(field int, delimiter string) func(string) (netip.Prefix, error) {
	return func(line string) (netip.Prefix, error) {
		// Split line into fields
		fields := strings.Split(line, delimiter)
		if field > len(fields) {
			return netip.Prefix{}, fmt.Errorf("Field %d not found in line: %s", field, line)
		}
		return cq.ParsePrefixOrAddr(fields[field-1])
	}
}

func handleFilter(c *cli.Context) error {
	var err error
	var excludePrefixSet, matchPrefixSet *netipmap.PrefixSet
	// TODO remove this once we can do everything with PrefixSets
	var excludeIpset *netipx.IPSet

	// -exclude
	if excludePath := c.String("exclude"); excludePath != "" {
		excludePrefixSet, err = cq.LoadPrefixSetFromFile(excludePath, errorHandler)
		if err != nil {
			return err
		}
		excludeIpsb := netipx.IPSetBuilder{}
		for _, prefix := range excludePrefixSet.Prefixes() {
			excludeIpsb.AddPrefix(prefix)
		}
		excludeIpset, err = excludeIpsb.IPSet()
		if err != nil {
			return err
		}

	}

	// -match
	if matchPath := c.String("match"); matchPath != "" {
		matchPsb, err := cq.LoadPrefixSetBuilderFromFile(matchPath, errorHandler)
		if err != nil {
			return err
		}
		if excludePrefixSet != nil {
			for _, excludePrefix := range excludePrefixSet.Prefixes() {
				// TODO this needs to be RemoveDescendants
				matchPsb.Remove(excludePrefix)
			}
		}
		matchPrefixSet = matchPsb.PrefixSet()
	}

	// Set up parser
	var parser func(string) (netip.Prefix, error)
	field := c.Int("field")
	if field != 0 {
		delimiter := c.String("delimiter")
		if delimiter == "\\t" {
			delimiter = "\t"
		}
		parser = parseLine(field, delimiter)
	} else {
		parser = cq.ParsePrefixOrAddr
	}

	// set up processor
	p := cq.CidrProcessor{
		ParseFn: parser,
		ErrFn:   errorHandler,
		HandlerFn: func(prefix netip.Prefix, line string) error {
			// Skip prefix if it doesn't overlap with the match list
			if matchPrefixSet != nil && !matchPrefixSet.OverlapsPrefix(prefix) {
				return nil
			}

			// Apply exclusions
			if excludePrefixSet != nil {
				if excludePrefixSet.Encompasses(prefix) {
					return nil
				}
				if excludePrefixSet.OverlapsPrefix(prefix) {
					// If we're printing full lines, only print the line once.
					// If we're just printing plain CIDRs, then subtract the
					// excluded portion and print the remainder.
					if field != 0 {
						fmt.Println(line)
					} else {
						// TODO reimplement with PrefixMap
						remaining, err := cq.PrefixMinusIPSet(prefix, excludeIpset)
						if err != nil {
							return err
						}
						for _, p := range remaining {
							fmt.Println(p)
						}
					}
					return nil
				}
			}

			// Prefix made it through the filter unaffected
			fmt.Println(line)
			return nil
		},
	}

	return iterPathArgs(c, func(r io.Reader) error {
		return p.Process(r)
	})
}

func main() {
	// set args for examples sake
	app := &cli.App{
		Name:                   "cidrq",
		Usage:                  "CIDR manipulation tool",
		UseShortOptionHandling: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "err",
				Aliases: []string{"e"},
				Usage:   "Action to take on error (abort, skip, warn)",
				Value:   AbortOnError,
			},
		},
		Before: func(c *cli.Context) error {
			return setErrorHandler(c)
		},
		Commands: []*cli.Command{
			{
				Name:      "merge",
				Usage:     "Merge lists of CIDRs into one",
				Aliases:   []string{"m"},
				ArgsUsage: "[paths]",
				Action:    handleMerge,
			},
			{
				Name:      "filter",
				Usage:     "Filter lists of CIDRs",
				Aliases:   []string{"f"},
				ArgsUsage: "[paths]",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "exclude",
						Aliases: []string{"x"},
						Usage: "Path to CIDR exclusion list. These CIDRs will " +
							"be omitted from the output. This may require splitting " +
							"input CIDRs.",
						Action: validatePath,
					},
					&cli.StringFlag{
						Name:    "match",
						Aliases: []string{"m"},
						Usage: "Path to CIDR match list. The filter will permit " +
							"any CIDRs which overlap with the match list. If " +
							"-exclude is provided, it will be applied after matching.",
						Action: validatePath,
					},
					&cli.IntFlag{
						Name:    "field",
						Aliases: []string{"f"},
						Usage: "Instruct cidrq to extract CIDR from a field, " +
							" where field delimiter is provided via -d. Field " +
							"extraction does not apply to exclusion or match lists.",
					},
					&cli.StringFlag{
						Name:    "delimiter",
						Aliases: []string{"d"},
						Usage:   "Delimiter for field separation (use '\\t' for tab).",
					},
				},
				Action: handleFilter,
			},
		},
	}

	//defer profile.Start(profile.ProfilePath(".")).Stop()

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
