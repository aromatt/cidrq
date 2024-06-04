package main

import (
	"fmt"
	cli "github.com/urfave/cli/v2"
	"io"
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
		ValParser: cq.ParsePrefixOrAddr,
		HandlerFn: func(parsed *cq.ParsedLine) error {
			for _, prefix := range parsed.Prefixes {
				merged.Add(prefix)
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

	// Output merged CIDRs
	mergedIPs := merged.PrefixSet()
	cq.Logf("Done loading CIDRs\n")
	for _, p := range mergedIPs.Prefixes() {
		fmt.Println(cq.StringMaybeAddr(p))
	}
	return nil
}

// TODO: introduce different filter modes (current behavior is --inclusive)
//
//	--inclusive: print prefix if match.overlaps(p) && !exclude.encompasses(p)
//	--exclusive: print prefix if match.encompasses(p) && !exclude.overlaps(p)
//	--selective(*): print match.intersection(p).subtract(exclude)
//
// (*): though this should probably be its own subcommand
//
// TODO: filter should only output <= 1 line per input line.
func handleFilter(c *cli.Context) error {
	var err error
	var excludePrefixSet, matchPrefixSet *netipds.PrefixSet

	quiet := c.Bool("quiet")
	clean := c.Bool("clean")

	if !quiet {
		// --exclude
		if excludePath := c.String("exclude"); excludePath != "" {
			cq.Logf("Loading exclude file '%s'\n", c.String("exclude"))
			excludePrefixSet, err = cq.LoadPrefixSetFromFile(excludePath, errorHandler)
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
			if excludePrefixSet != nil {
				for _, excludePrefix := range excludePrefixSet.Prefixes() {
					matchPsb.Subtract(excludePrefix)
				}
			}
			matchPrefixSet = matchPsb.PrefixSet()
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

				// Skip the prefix if it doesn't overlap with the match list.
				if matchPrefixSet != nil && !matchPrefixSet.OverlapsPrefix(p) {
					continue
				}

				// Skip the prefix if it's encompassed by the exclude list.
				// TODO: this should definitely be "covered by", but PrefixSet
				// doesn't have that yet.
				if excludePrefixSet != nil && excludePrefixSet.Encompasses(p) {
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
					&cli.BoolFlag{
						Name:    "quiet",
						Aliases: []string{"q"},
						Usage:   "Suppress stdout. If err == print, err lines are still printed.",
						Value:   false,
					},
					&cli.BoolFlag{
						Name:    "clean",
						Aliases: []string{"c"},
						Usage:   "Replace selected fields with their respective parsed CIDRs",
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
