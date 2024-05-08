package main

import (
	//"flag"
	"bufio"
	"fmt"
	cli "github.com/urfave/cli/v2"
	"go4.org/netipx"
	"net/netip"
	"os"
)

func loadPrefixesFromFile(path string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Error opening file %s: %v", path, err)
	}
	defer file.Close()
	for {
		var cidr string
		_, err := fmt.Fscanln(file, &cidr)
		if err != nil {
			break
		}
		p, err := netip.ParsePrefix(EnsurePrefix(cidr))
		if err != nil {
			return nil, fmt.Errorf("Error parsing CIDR %s: %v", cidr, err)
		}
		prefixes = append(prefixes, p)
	}
	return prefixes, nil
}

// TODO reimplement with IPMap
func handleMerge(c *cli.Context) error {
	merged := netipx.IPSetBuilder{}
	if c.Args().Len() == 0 || (c.Args().Len() == 1 && c.Args().First() == "-") {
		// Read from stdin
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := scanner.Text()
			if text == "" {
				continue
			}
			p, err := netip.ParsePrefix(EnsurePrefix(text))
			if err != nil {
				return fmt.Errorf("Error parsing CIDR %s: %v", text, err)
			}
			merged.AddPrefix(p)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("Error reading from stdin: %v", err)
		}
	} else {
		// Read from each file specified
		for _, path := range c.Args().Slice() {
			prefixes, err := loadPrefixesFromFile(path)
			if err != nil {
				return err
			}
			for _, p := range prefixes {
				merged.AddPrefix(p)
			}
		}
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

func main() {
	// set args for examples sake
	app := &cli.App{
		Name:  "cidrq",
		Usage: "CIDR manipulation tool",
		Commands: []*cli.Command{
			{
				Name:      "merge",
				Usage:     "Merge lists of CIDRs into one",
				Aliases:   []string{"m"},
				ArgsUsage: "[paths or - for stdin]",
				Action:    handleMerge,
			},
			{
				Name:      "filter",
				Usage:     "Filter out CIDRs using exclusion lists",
				Aliases:   []string{"m"},
				ArgsUsage: "[paths or - for stdin]",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "exclude",
						Aliases: []string{"b"},
						Usage:   "Path to CIDR exclusion list",
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
