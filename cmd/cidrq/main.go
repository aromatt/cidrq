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
	"log"
	//profile "github.com/pkg/profile"
)

const (
	AbortOnError = "abort"
	WarnOnError  = "warn"
	SkipOnError  = "skip"
)

var errorHandler func(error) error
var logf func(string, ...any)

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

func setVerbose(c *cli.Context) {
	v := c.Bool("verbose")
	if v {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
		logf = func(format string, v ...any) {
			log.Printf(format, v...)
		}
	} else {
		logf = func(format string, v ...any) {}
	}
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

// TODO reimplement with IPMap
func handleMerge(c *cli.Context) error {
	merged := netipx.IPSetBuilder{}

	// Set up processor
	p := cq.CidrProcessor{
		ParseFn: cq.ParsePrefixOrAddr,
		HandlerFn: func(prefix netip.Prefix, line string) error {
			merged.AddPrefix(prefix)
			return nil
		},
		ErrFn: errorHandler,
	}

	logf("Loading input CIDRs\n")
	err := iterPathArgs(c, func(r io.Reader) error {
		return p.Process(r)
	})
	if err != nil {
		return err
	}

	// Output merged CIDRs
	mergedIPs, err := merged.IPSet()
	if err != nil {
		return fmt.Errorf("Error merging IPs: %v", err)
	}

	logf("Done loading CIDRs\n")
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

// TODO reimplement with IPMap
func handleFilter(c *cli.Context) error {
	var err error
	var excludeIpset, matchIpset *netipx.IPSet

	// -exclude
	if excludePath := c.String("exclude"); excludePath != "" {
		logf("Loading exclude file '%s'\n", c.String("exclude"))
		excludeIpset, err = cq.LoadIPSetFromFile(excludePath, errorHandler)
		if err != nil {
			return err
		}
	}

	// -match
	if matchPath := c.String("match"); matchPath != "" {
		logf("Loading match file '%s'\n", c.String("match"))
		matchIpsb, err := cq.LoadIPSetBuilderFromFile(matchPath, errorHandler)
		if err != nil {
			return err
		}
		if excludeIpset != nil {
			matchIpsb.RemoveSet(excludeIpset)
		}
		matchIpset, err = matchIpsb.IPSet()
		if err != nil {
			return err
		}
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
			if matchIpset != nil && !matchIpset.OverlapsPrefix(prefix) {
				return nil
			}

			// Apply exclusions
			if excludeIpset != nil {
				if excludeIpset.OverlapsPrefix(prefix) {
					// Just skip if the entire prefix is excluded
					if excludeIpset.ContainsPrefix(prefix) {
						return nil
					}
					// Subtract the excluded portion and print the remainder
					remaining, err := cq.PrefixMinusIPSet(prefix, excludeIpset)
					if err != nil {
						return err
					}
					for range remaining {
						fmt.Println(line)
					}
					return nil
				}
			}

			// Prefix made it through the filter unaffected
			fmt.Println(line)
			return nil
		},
	}

	logf("Processing input CIDRs\n")
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
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Print logs to stderr",
				Value:   false,
			},
		},
		Before: func(c *cli.Context) error {
			setVerbose(c)
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

	logf("Done\n")
}
