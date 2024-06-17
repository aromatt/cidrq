package main

import (
	"fmt"
	cli "github.com/urfave/cli/v2"
	"io"
	"os"

	cq "github.com/aromatt/cidrq/pkg"
	"github.com/aromatt/netipds"
	"net/netip"
	//profile "github.com/pkg/profile"
)

// Error handling

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

// Reduce operations

type ReduceOpFn func(*netipds.PrefixSet, *netipds.PrefixSet) *netipds.PrefixSet

type ReduceOp struct {
	OpFn ReduceOpFn
	Path string
}

// TODO
//var Subtract ReduceOpFn = func(a, b *netipds.PrefixSet) *netipds.PrefixSet {
//	return a.Subtract(b)
//}
//
//var Intersect ReduceOpFn = func(a, b *netipds.PrefixSet) *netipds.PrefixSet {
//	return a.Intersect(b)
//}
//
//var Union ReduceOpFn = func(a, b *netipds.PrefixSet) *netipds.PrefixSet {
//	return a.Union(b)
//}

var reduceOps = []ReduceOp{}

// Validation functions

func validatePath(c *cli.Context, v string) error {
	if _, err := os.Stat(v); os.IsNotExist(err) {
		return fmt.Errorf("File %s does not exist", v)
	}
	return nil
}

func validateExcludeMode(c *cli.Context, v string) error {
	if !(v == "overlap" || v == "encompass") {
		return fmt.Errorf("Invalid exclude mode: '%s'", v)
	}
	return nil
}

func validateMatchMode(c *cli.Context, v string) error {
	if !(v == "overlap" || v == "encompass") {
		return fmt.Errorf("Invalid match mode: '%s'", v)
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

// Subcommand handlers

func handleReduce(c *cli.Context) error {
	reduced := netipds.PrefixSetBuilder{}

	// Set up processor
	p := cq.CidrProcessor{
		ValParser: cq.ParsePrefixOrAddr,
		HandlerFn: func(parsed *cq.ParsedLine) error {
			for _, prefix := range parsed.Prefixes {
				// TODO need a way to merge redundant prefixes
				reduced.Add(prefix)
			}
			return nil
		},
		ErrFn: errorHandler,
	}

	cq.Logf("Loading input CIDRs\n")
	err := iterPathArgs(c, func(r io.Reader) error {
		return p.Process(r)
	})
	if err != nil {
		return err
	}

	// Output reduced CIDR set
	reducedPrefixSet := reduced.PrefixSet()
	cq.Logf("Done loading CIDRs\n")
	for _, p := range reducedPrefixSet.PrefixesCompact() {
		fmt.Println(cq.StringMaybeAddr(p))
	}
	return nil
}

func prefixSetMembershipFn(mode string) func(*netipds.PrefixSet, netip.Prefix) bool {
	switch mode {
	case "overlap":
		return func(ps *netipds.PrefixSet, p netip.Prefix) bool {
			return ps.OverlapsPrefix(p)
		}
	case "encompass":
		return func(ps *netipds.PrefixSet, p netip.Prefix) bool {
			return ps.Encompasses(p)
		}
	// TODO add "cover" mode - returns true if the prefix can be covered by any
	// subset of prefixes in the set. Do this once PrefixSet has a Covers method.
	default:
		panic("Invalid mode")
	}
}

func handleFilter(c *cli.Context) error {
	var err error
	var excludeSet, matchSet *netipds.PrefixSet

	quiet := c.Bool("quiet")
	clean := c.Bool("clean")
	matchFn := prefixSetMembershipFn(c.String("match-mode"))
	excludeFn := prefixSetMembershipFn(c.String("exclude-mode"))

	if !quiet {
		// --exclude
		if excludePath := c.String("exclude"); excludePath != "" {
			cq.Logf("Loading exclude file '%s'\n", c.String("exclude"))
			excludeSet, err = cq.LoadPrefixSetFromFile(excludePath, errorHandler)
			if err != nil {
				return err
			}
		}

		// --match
		if matchPath := c.String("match"); matchPath != "" {
			cq.Logf("Loading match file '%s'\n", c.String("match"))
			matchPsb, err := cq.LoadPrefixSetBuilderFromFile(matchPath, errorHandler)
			if err != nil {
				return err
			}
			if excludeSet != nil {
				for _, excludePrefix := range excludeSet.Prefixes() {
					matchPsb.SubtractPrefix(excludePrefix)
				}
			}
			matchSet = matchPsb.PrefixSet()
		}
	}

	// Set up processor
	fields := c.IntSlice("field")
	pr := cq.CidrProcessor{
		Fields:    fields,
		Delimiter: c.String("delimiter"),
		ValParser: cq.ValParser(c.Bool("url"), c.Bool("port")),
		ErrFn:     errorHandler,
		HandlerFn: func(parsed *cq.ParsedLine) error {
			anyPassed := false
			for _, p := range parsed.Prefixes {
				// In quiet mode, all the filter does is parse and validate input.
				if quiet {
					continue
				}

				// Skip the prefix if it doesn't match any match lists.
				if matchSet != nil && !matchFn(matchSet, p) {
					continue
				}

				// Skip the prefix if it matches an exclude list.
				if excludeSet != nil && excludeFn(excludeSet, p) {
					continue
				}
				anyPassed = true
			}

			if !anyPassed {
				return nil
			}

			if clean {
				fmt.Println(parsed.Clean())
			} else {
				fmt.Println(parsed.Raw)
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
				Usage: "Action to take on error (abort, skip, warn, print). " +
					"In print mode, the line is printed to stdout. " +
					"Default: abort.",
				Value: AbortOnError,
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Print verbose logs to stderr. Default: false.",
				Value:   false,
			},
		},
		Before: func(c *cli.Context) error {
			setVerbose(c)
			return setErrorHandler(c)
		},
		Commands: []*cli.Command{
			{
				Name:  "reduce",
				Usage: "Combine lists of CIDRs",
				Description: "Provide files containing lists of CIDRs using " +
					"--union (-u), --intersect (-i), or --subtract (-s). CIDR " +
					"lists are processed in the order they are provided. The " +
					"output is a compacted list of CIDRs.",
				Aliases:   []string{"r"},
				ArgsUsage: "[paths]",
				Action:    handleReduce,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "union",
						Aliases: []string{"u"},
						Usage: "Return the union of these CIDRs with the " +
							"working set.",
						Action: func(c *cli.Context, v string) error {
							// TODO
							//reduceOps = append(reduceOps, ReduceOp{OpFn: Union, Path: v})
							return nil
						},
					},
					&cli.StringFlag{
						Name:    "intersect",
						Aliases: []string{"i"},
						Usage: "Return the intersection of these CIDRs with the " +
							"working set.",
						Action: func(c *cli.Context, v string) error {
							// TODO
							//reduceOps = append(reduceOps, ReduceOp{OpFn: Intersect, Path: v})
							return nil
						},
					},
					&cli.StringFlag{
						Name:    "subtract",
						Aliases: []string{"s"},
						Usage: "Subtract these CIDRs from the working set. " +
							"If a subtracted CIDR is a child of a working-set " +
							"CIDR, then the working-set CIDR will be split, " +
							"leaving behind the remaining portion.",
						Action: func(c *cli.Context, v string) error {
							// TODO
							//reduceOps = append(reduceOps, ReduceOp{OpFn: Subtract, Path: v})
							return nil
						},
					},
				},
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
							"be omitted from the output. This may require " +
							"splitting input CIDRs.",
						Action: validatePath,
					},
					&cli.StringFlag{
						Name: "exclude-mode",
						Usage: "Comparison strategy for exclude list (overlap, " +
							"encompass). In overlap mode, an input CIDR is " +
							"excluded if it overlaps any CIDR in an exclude list. " +
							"In encompass mode, an input CIDR is only excluded if " +
							"it has a parent in an exclude list. Default: encompass.",
						Action: validateExcludeMode,
					},
					&cli.StringFlag{
						Name:    "match",
						Aliases: []string{"m"},
						Usage: "Path to CIDR match list. The filter will permit " +
							"any CIDRs which overlap with the match list. If " +
							"-exclude is provided, it will be applied after " +
							"matching.",
						Action: validatePath,
					},
					&cli.StringFlag{
						Name: "match-mode",
						Usage: "Comparison strategy for match list (overlap, " +
							"encompass). In overlap mode, an input CIDR passes " +
							"the filter if it overlaps any CIDR in a match list. " +
							"In encompass mode, an input CIDR passes only if it " +
							"has a parent in a match list. Default: overlap.",
						Action: validateMatchMode,
					},
					&cli.IntSliceFlag{
						Name:    "field",
						Aliases: []string{"f"},
						Usage: "Instruct cidrq to look for CIDRs in one or more " +
							"fields, where field delimiter is provided via -d. " +
							"Parsing is performed only on input CIDRs, not " +
							"exclusion or match lists.",
					},
					&cli.StringFlag{
						Name:    "delimiter",
						Aliases: []string{"d"},
						Usage: "Delimiter for field separation (use '\\t' for " +
							"tab).",
					},
					&cli.BoolFlag{
						Name:    "quiet",
						Aliases: []string{"q"},
						Usage: "Suppress stdout. If err == print, err lines " +
							"are still printed.",
						Value: false,
					},
					&cli.BoolFlag{
						Name:    "clean",
						Aliases: []string{"c"},
						Usage: "Replace selected fields with their respective " +
							"parsed CIDRs",
						Value: false,
					},
					&cli.BoolFlag{
						Name:    "url",
						Aliases: []string{"u"},
						Usage: "Accept a URL as valid if the hostname is a " +
							"valid IP.",
					},
					&cli.BoolFlag{
						Name:    "port",
						Aliases: []string{"p"},
						Usage: "Accept a host[:port] as valid if the host is " +
							"a valid IP.",
					},
					&cli.BoolFlag{
						Name:    "flat",
						Aliases: []string{"F"},
						// TODO apply exclusion list after matching lines,
						// before printing
						Usage: "Print each matched CIDR on a separate line.",
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
