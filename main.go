package main

import (
	"fmt"
	"io"
	"net/netip"
	"os"

	"github.com/aromatt/netipds"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
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
		errorHandler = func(l string, e error) error { return e }
	case WarnOnError:
		errorHandler = func(l string, e error) error {
			if e != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", e)
			}
			return nil
		}
	case SkipOnError:
		errorHandler = func(l string, e error) error { return nil }
	case PrintOnError:
		errorHandler = func(l string, e error) error {
			if e != nil {
				fmt.Println(l)
			}
			return nil
		}
	default:
		return fmt.Errorf("Invalid error action %s", v)
	}
	return nil
}

// Combine operations

// CombineOpFn performs an operation on a PrefixSetBuilder using a PrefixSet as
// input.
type CombineOpFn func(*netipds.PrefixSetBuilder, *netipds.PrefixSet)

type CombineOp struct {
	OpFn CombineOpFn
	Path string
}

var Subtract CombineOpFn = func(a *netipds.PrefixSetBuilder, b *netipds.PrefixSet) {
	a.Subtract(b)
}

var Intersect CombineOpFn = func(a *netipds.PrefixSetBuilder, b *netipds.PrefixSet) {
	a.Intersect(b)
}

var Union CombineOpFn = func(a *netipds.PrefixSetBuilder, b *netipds.PrefixSet) {
	a.Merge(b)
}

var combineOps = []CombineOp{}

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

// iterPathArgs calls fn on io.Readers for stdin and each of c.Args()
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

// prefixSetMembershipFn returns a function which calls the method of PrefixSet
// corresponding to the provided mode.
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

// PrefixSetBuilderCidrProcessor creates a CidrProcessor which uses
// ParsePrefixOrAddr as its ValParser, adds all parsed prefixes to a new
// PrefixSetBuilder (which is also returned as the second return value), and
// uses the global errorHandler.
func PrefixSetBuilderCidrProcessor() (*CidrProcessor, *netipds.PrefixSetBuilder) {
	psb := netipds.PrefixSetBuilder{}
	cp := CidrProcessor{
		ValParser: ParsePrefixOrAddr,
		HandlerFn: func(parsed *ParsedLine) error {
			for _, prefix := range parsed.Prefixes {
				psb.Add(prefix)
			}
			return nil
		},
		ErrFn: errorHandler,
	}
	return &cp, &psb
}

// Subcommand handlers

func handleCombine(c *cli.Context) error {
	var err error

	// Build initial working set by parsing CIDRs from stdin
	cp, workingPsb := PrefixSetBuilderCidrProcessor()
	if err = cp.Process(io.Reader(os.Stdin)); err != nil {
		return err
	}

	// Iterate over combineOps, applying each to the working set
	for _, op := range combineOps {
		cp, opPsb := PrefixSetBuilderCidrProcessor()
		opReader, err := os.Open(op.Path)
		if err != nil {
			return err
		}
		if err = cp.Process(opReader); err != nil {
			return err
		}
		op.OpFn(workingPsb, opPsb.PrefixSet())
	}

	// Output combined CIDR set
	combinedPs := workingPsb.PrefixSet()
	for _, p := range combinedPs.PrefixesCompact() {
		fmt.Println(StringMaybeAddr(p))
	}
	return nil
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
			Logf("Loading exclude file '%s'\n", c.String("exclude"))
			excludeSet, err = LoadPrefixSetFromFile(excludePath, errorHandler)
			if err != nil {
				return err
			}
		}

		// --match
		if matchPath := c.String("match"); matchPath != "" {
			Logf("Loading match file '%s'\n", c.String("match"))
			matchPsb, err := LoadPrefixSetBuilderFromFile(matchPath, errorHandler)
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
	pr := CidrProcessor{
		Fields:    fields,
		Delimiter: c.String("delimiter"),
		ValParser: ValParser(c.Bool("url"), c.Bool("host")),
		ErrFn:     errorHandler,
		HandlerFn: func(parsed *ParsedLine) error {
			anyPassed := false
			for _, p := range parsed.Prefixes {
				// In quiet mode, the filter just parses and validates input CIDRs.
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

	Logf("Processing input CIDRs\n")
	return iterPathArgs(c, func(r io.Reader) error {
		return pr.Process(r)
	})
}

func handleSort(c *cli.Context) error {
	sorted := netipds.PrefixSetBuilder{}

	// Set up processor
	p := CidrProcessor{
		ValParser: ParsePrefixOrAddr,
		HandlerFn: func(parsed *ParsedLine) error {
			for _, prefix := range parsed.Prefixes {
				// TODO need a way to merge redundant prefixes
				sorted.Add(prefix)
			}
			return nil
		},
		ErrFn: errorHandler,
	}

	Logf("Loading input CIDRs\n")
	err := iterPathArgs(c, func(r io.Reader) error {
		return p.Process(r)
	})
	if err != nil {
		return err
	}

	// Output sorted CIDR set
	sortedPrefixSet := sorted.PrefixSet()
	Logf("Done loading CIDRs\n")
	for _, p := range sortedPrefixSet.Prefixes() {
		fmt.Println(StringMaybeAddr(p))
	}
	return nil
}

func main() {

	app := &cli.App{
		Name:  "cidrq",
		Usage: "CIDR manipulation tool",
		Authors: []*cli.Author{
			{
				Name:  "Andrew Matteson",
				Email: "andrew.k.matteson@gmail.com",
			},
		},
		UseShortOptionHandling: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "err",
				Aliases: []string{"e"},
				Usage: "Action to take on error (abort, skip, warn, print). " +
					"In print mode, the input line is printed to stdout. " +
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
			SetVerbose(c.Bool("verbose"))
			return setErrorHandler(c)
		},
		Commands: []*cli.Command{
			{
				Name:  "combine",
				Usage: "Combine lists of CIDRs",
				Description: "Provide files containing lists of CIDRs using " +
					"--union (-u), --intersect (-i), or --subtract (-s). Operations " +
					"are performed in the order they are provided, with stdin " +
					"as the first list. The output is a compacted list of CIDRs.",
				Aliases:   []string{"c"},
				ArgsUsage: "[paths]",
				Action:    handleCombine,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "union",
						Aliases: []string{"u"},
						Usage: "Return the union of the working set with the " +
							"CIDRs in `FILE`.",
						TakesFile: true,
						Action: func(c *cli.Context, v string) error {
							combineOps = append(combineOps, CombineOp{
								OpFn: Union,
								Path: v,
							})
							return nil
						},
					},
					&cli.StringFlag{
						Name:    "intersect",
						Aliases: []string{"i"},
						Usage: "Return the intersection of the working set " +
							"with the CIDRs in `FILE`.",
						TakesFile: true,
						Action: func(c *cli.Context, v string) error {
							combineOps = append(combineOps, CombineOp{
								OpFn: Intersect,
								Path: v,
							})
							return nil
						},
					},
					&cli.StringFlag{
						Name:    "subtract",
						Aliases: []string{"s"},
						Usage: "Subtract the CIDRs in `FILE` from the working " +
							"set. If a subtracted CIDR is a child of a " +
							"working-set CIDR, then the working-set CIDR will " +
							"be split, leaving behind the remaining portion.",
						TakesFile: true,
						Action: func(c *cli.Context, v string) error {
							combineOps = append(combineOps, CombineOp{
								OpFn: Subtract,
								Path: v,
							})
							return nil
						},
					},
				},
			},
			{
				Name:      "sort",
				Usage:     "Sort lists of CIDRs",
				Aliases:   []string{"s"},
				ArgsUsage: "[paths]",
				Action:    handleSort,
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
						Usage: "Path to CIDR exclusion list. Input lines " +
							"containing CIDRs found in `FILE` will be omitted " +
							"from the output.",
						Action: validatePath,
					},
					&cli.StringFlag{
						Name:  "exclude-mode",
						Value: "encompass",
						Usage: "Comparison strategy for exclude list (overlap, " +
							"encompass). In overlap mode, an input CIDR is " +
							"excluded if it overlaps any CIDR in an exclude " +
							"list. In encompass mode, an input CIDR is only " +
							"excluded if it has a parent in an exclude list. " +
							"Default: encompass.",
						Action: validateExcludeMode,
					},
					&cli.StringFlag{
						Name:    "match",
						Aliases: []string{"m"},
						Usage: "Path to CIDR match list. The filter will permit " +
							"any input lines containing CIDRs that match any of " +
							"the CIDRs in `FILE`. If -exclude is provided, it will " +
							"be applied after matching.",
						Action: validatePath,
					},
					&cli.StringFlag{
						Name:  "match-mode",
						Value: "overlap",
						Usage: "Comparison strategy for match list (overlap, " +
							"encompass). In overlap mode, an input CIDR is " +
							"a match if it overlaps any CIDR in a match list. " +
							"In encompass mode, an input CIDR matches only if " +
							"it has a parent in a match list. Default: overlap.",
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
						Usage: "Suppress stdout. If -err == print, error lines " +
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
						Name:    "host",
						Aliases: []string{"H"},
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

	cli.CommandHelpTemplate = `NAME
      {{template "helpNameTemplate" .}}

USAGE
      {{template "usageTemplate" .}}{{if .Category}}

CATEGORY
      {{.Category}}{{end}}{{if .Description}}

DESCRIPTION
      {{wrap .Description 6}}{{end}}{{if .VisibleFlagCategories}}

OPTIONS{{template "visibleFlagCategoryTemplate" .}}{{else if .VisibleFlags}}

OPTIONS{{range $i, $e := .VisibleFlags}}
      {{prefixedNames $e}}
            {{wrap $e.Usage 12}}
{{end}}{{end}}
`

	maxLineLength := getTerminalWidth(80) - 8
	cli.HelpPrinter = func(w io.Writer, templ string, data interface{}) {
		funcMap := map[string]interface{}{
			"wrapAt": func() int { return maxLineLength },
			"prefixedNames": func(f cli.Flag) string {
				df, ok := f.(cli.DocGenerationFlag)
				if !ok {
					return ""
				}
				placeholder, _ := unquoteUsage(df.GetUsage())
				needsPlaceholder := df.TakesValue()

				if needsPlaceholder && placeholder == "" {
					placeholder = "value"
				}

				return cli.FlagNamePrefixer(f.Names(), placeholder)
			},
		}
		cli.HelpPrinterCustom(w, templ, data, funcMap)
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	Logf("Done\n")
}

// Returns the placeholder, if any, and the unquoted usage string.
// Copied from urfave/cli
func unquoteUsage(usage string) (string, string) {
	for i := 0; i < len(usage); i++ {
		if usage[i] == '`' {
			for j := i + 1; j < len(usage); j++ {
				if usage[j] == '`' {
					name := usage[i+1 : j]
					usage = usage[:i] + name + usage[j+1:]
					return name, usage
				}
			}
			break
		}
	}
	return "", usage
}

func getTerminalWidth(fallback int) int {
	fd := int(os.Stdout.Fd())
	if width, _, err := term.GetSize(fd); err == nil {
		return width
	}
	return fallback
}
