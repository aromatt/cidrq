package main

import (
	"fmt"
	cli "github.com/urfave/cli/v2"
	"io"
	"net/netip"
	"os"

	cq "github.com/aromatt/cidrq/pkg"
	"github.com/aromatt/netipds"
	//profile "github.com/pkg/profile"
)

const (
	AbortOnError = "abort"
	WarnOnError  = "warn"
	SkipOnError  = "skip"
	PrintOnError = "print"
)

var errorHandler func(string, error) error

func setErrorHandler(c *cli.Context) error {
	v := c.String("err")
	switch v {
	case AbortOnError:
		errorHandler = func(line string, err error) error { return err }
	case WarnOnError:
		errorHandler = func(line string, err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			}
			return nil
		}
	case SkipOnError:
		errorHandler = func(line string, err error) error { return nil }
	case PrintOnError:
		errorHandler = func(line string, err error) error {
			if err != nil {
				fmt.Println(line)
			}
			return nil
		}
	default:
		return fmt.Errorf("Invalid error action %s", v)
	}
	return nil
}

func setVerbose(c *cli.Context) {
	cq.SetVerbose(c.Bool("verbose"))
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
	merged := netipds.PrefixSetBuilder{}

	// Set up processor
	p := cq.CidrProcessor{
		LineParser: cq.ToSliceOfOneFn(cq.ParsePrefixOrAddr),
		HandlerFn: func(prefixes []netip.Prefix, line string) error {
			for _, prefix := range prefixes {
				merged.Add(prefix)
			}
			return nil
		},
		ErrFn: errorHandler,
	}

	//fmt.Println(merged.String())

	cq.Logf("Loading input CIDRs\n")
	err := iterPathArgs(c, func(r io.Reader) error {
		return p.Process(r)
	})
	if err != nil {
		return err
	}

	// Output merged CIDRs
	mergedIPs := merged.PrefixSet()
	cq.Logf("Done loading CIDRs\n")
	for _, p := range mergedIPs.Prefixes() {
		fmt.Println(p)
	}
	return nil
}

func handleValidate(c *cli.Context) error {
	// Set up parser
	var lineParser func(string) ([]netip.Prefix, error)
	valParser := cq.ValParser(c.Bool("url"), c.Bool("port"))
	fields := c.IntSlice("field")
	if len(fields) != 0 {
		delimiter := c.String("delimiter")
		if delimiter == "\\t" {
			delimiter = "\t"
		}
		lineParser = cq.LineParser(fields, delimiter, valParser)
	} else {
		lineParser = cq.ToSliceOfOneFn(valParser)
	}

	quiet := c.Bool("quiet")

	// Set up processor
	pr := cq.CidrProcessor{
		LineParser: lineParser,
		ErrFn:      errorHandler,
		HandlerFn: func(prefixes []netip.Prefix, line string) error {
			if !quiet {
				fmt.Println(line)
			}
			return nil
		},
	}

	cq.Logf("Processing input CIDRs\n")
	return iterPathArgs(c, func(r io.Reader) error {
		return pr.Process(r)
	})
}

func handleFilter(c *cli.Context) error {
	var err error
	var excludePrefixSet, matchPrefixSet *netipds.PrefixSet

	// -exclude
	if excludePath := c.String("exclude"); excludePath != "" {
		cq.Logf("Loading exclude file '%s'\n", c.String("exclude"))
		excludePrefixSet, err = cq.LoadPrefixSetFromFile(excludePath, errorHandler)
		if err != nil {
			return err
		}
	}

	// -match
	if matchPath := c.String("match"); matchPath != "" {
		cq.Logf("Loading match file '%s'\n", c.String("match"))
		matchPsb, err := cq.LoadPrefixSetBuilderFromFile(matchPath, errorHandler)
		if err != nil {
			return err
		}
		if excludePrefixSet != nil {
			for _, excludePrefix := range excludePrefixSet.Prefixes() {
				matchPsb.Subtract(excludePrefix)
			}
		}
		matchPrefixSet = matchPsb.PrefixSet()
	}

	// Set up parser
	var lineParser func(string) ([]netip.Prefix, error)
	valParser := cq.ValParser(c.Bool("url"), c.Bool("port"))
	fields := c.IntSlice("field")
	if len(fields) != 0 {
		delimiter := c.String("delimiter")
		if delimiter == "\\t" {
			delimiter = "\t"
		}
		lineParser = cq.LineParser(fields, delimiter, valParser)
	} else {
		lineParser = cq.ToSliceOfOneFn(valParser)
	}

	// Set up processor
	pr := cq.CidrProcessor{
		LineParser: lineParser,
		ErrFn:      errorHandler,
		HandlerFn: func(prefixes []netip.Prefix, line string) error {
			for _, p := range prefixes {
				// Skip the prefix if it doesn't overlap with the match list.
				// (Skip the whole line if none of its prefixes match.)
				if matchPrefixSet != nil && !matchPrefixSet.OverlapsPrefix(p) {
					continue
				}

				// Apply exclusions
				if excludePrefixSet != nil {
					if excludePrefixSet.OverlapsPrefix(p) {
						// If the prefix is fully excluded, skip it.
						// (If all prefixes in the line are excluded, don't print
						// anything from the line.)
						if excludePrefixSet.Encompasses(p) {
							continue
						}
						// If we extracted CIDRs from fields, print the line once.
						// If each line was a single CIDR, then subtract the
						// excluded portion and print the remainder.
						if len(fields) > 0 {
							fmt.Println(line)
							return nil
						} else {
							remaining := excludePrefixSet.SubtractFromPrefix(p).Prefixes()
							for _, p := range remaining {
								fmt.Println(p)
							}
						}
						return nil
					}
				}
				// Prefix made it through the filter unaffected
				fmt.Println(line)
			}
			return nil
		},
	}

	cq.Logf("Processing input CIDRs\n")
	return iterPathArgs(c, func(r io.Reader) error {
		return pr.Process(r)
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
				Usage:   "Action to take on error (abort, skip, warn, print)",
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
				Name:      "validate",
				Usage:     "Validate CIDRs in input lines",
				Aliases:   []string{"v"},
				ArgsUsage: "[paths]",
				Flags: []cli.Flag{
					&cli.IntSliceFlag{
						Name:    "field",
						Aliases: []string{"f"},
						Usage: "Instruct cidrq to look for CIDRs in one or more " +
							"fields, where field delimiter is provided via -d. " +
							"Parsing is performed only on input CIDRs, not exclusion " +
							"or match lists.",
					},
					&cli.StringFlag{
						Name:    "delimiter",
						Aliases: []string{"d"},
						Usage:   "Delimiter for field separation (use '\\t' for tab).",
					},
					&cli.BoolFlag{
						Name:    "quiet",
						Aliases: []string{"q"},
						Usage:   "Suppress stdout. If err == print, err lines are still printed.",
						Value:   false,
					},
					&cli.BoolFlag{
						Name:    "url",
						Aliases: []string{"u"},
						Usage:   "Accept a URL as valid if the hostname is a valid IP.",
					},
					&cli.BoolFlag{
						Name:    "port",
						Aliases: []string{"p"},
						Usage:   "Accept a host[:port] as valid if the host is a valid IP.",
					},
				},
				Action: handleValidate,
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
					&cli.IntSliceFlag{
						Name:    "field",
						Aliases: []string{"f"},
						Usage: "Instruct cidrq to look for CIDRs in one or more " +
							"fields, where field delimiter is provided via -d. " +
							"Parsing is performed only on input CIDRs, not exclusion " +
							"or match lists.",
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

	cq.Logf("Done\n")
}
