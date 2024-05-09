package main

import (
	//"flag"
	"fmt"
	cli "github.com/urfave/cli/v2"
	"go4.org/netipx"
	"net/netip"
	"os"

	"github.com/aromatt/cidrq/util"
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
func iterInputCidrs(c *cli.Context, fn func(netip.Prefix) error) error {
	var err error

	// Assume stdin if no args
	if c.NArg() == 0 {
		if err = util.ReadCidrsFromStdin(fn, errorHandler); err != nil {
			return err
		}
	}

	// Iterate over files
	for _, path := range c.Args().Slice() {
		didReadStdin := false
		if path == "-" && !didReadStdin {
			didReadStdin = true
			if err = util.ReadCidrsFromStdin(fn, errorHandler); err != nil {
				return err
			}
		} else if err = util.ReadCidrsFromFile(path, fn, errorHandler); err != nil {
			return err
		}
	}

	return nil
}

// TODO reimplement with IPMap
func handleMerge(c *cli.Context) error {
	merged := netipx.IPSetBuilder{}
	err := iterInputCidrs(c, func(prefix netip.Prefix) error {
		merged.AddPrefix(prefix)
		return nil
	})
	if err != nil {
		return err
	}
	mergedIPs, err := merged.IPSet()
	if err != nil {
		return fmt.Errorf("Error merging IPs: %v", err)
	}
	for _, p := range mergedIPs.Prefixes() {
		fmt.Println(p)
	}
	return nil
}

// TODO reimplement with IPMap
func handleFilter(c *cli.Context) error {
	var err error
	var excludeIpset, matchIpset *netipx.IPSet

	// -exclude
	if excludePath := c.String("exclude"); excludePath != "" {
		excludeIpset, err = util.LoadIPSetFromFile(excludePath, errorHandler)
		if err != nil {
			return err
		}
	}

	// -match
	if matchPath := c.String("match"); matchPath != "" {
		matchIpsb, err := util.LoadIPSetBuilderFromFile(matchPath, errorHandler)
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

	return iterInputCidrs(c, func(prefix netip.Prefix) error {
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
				remaining, err := util.PrefixMinusIPSet(prefix, excludeIpset)
				if err != nil {
					return err
				}
				for _, p := range remaining {
					fmt.Println(p)
				}
				return nil
			}
		}

		// Prefix made it through the filter unaffected
		fmt.Println(prefix)
		return nil
	})
}

func main() {
	// set args for examples sake
	app := &cli.App{
		Name:  "cidrq",
		Usage: "CIDR manipulation tool",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "err",
				Usage: "Action to take on error (abort, skip, warn)",
				Value: AbortOnError,
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
						Aliases: []string{"e"},
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
				},
				Action: handleFilter,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
