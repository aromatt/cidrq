package main

import (
	//"flag"
	"fmt"
	cli "github.com/urfave/cli/v2"
	//"go4.org/netipx"
	"net/netip"
	"os"
)

// if the string does end with an explicit subnet mask, then append '/32'
func EnsurePrefix(s string) string {
	if s[len(s)-2] != '/' && s[len(s)-3] != '/' {
		return s + "/32"
	}
	return s
}

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

func handleIntersect(c *cli.Context) error {
	qPrefixes, err := loadPrefixesFromFile(c.String("query"))
	if err != nil {
		return err
	}
	rPrefixes, err := loadPrefixesFromFile(c.String("reference"))
	if err != nil {
		return err
	}
	fmt.Println(qPrefixes, rPrefixes)

	return nil
}

func main() {
	// set args for examples sake
	app := &cli.App{
		Name: "cidrq",
		Commands: []*cli.Command{
			{
				Name:    "cover",
				Aliases: []string{"c"},
				//Usage:       "",
				Description: "Given a reference list R and a query list Q, return the minimal subset of R that covers Q.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "reference",
						Aliases: []string{"r"},
						Usage:   "Path to the reference CIDR list",
					},
					&cli.StringFlag{
						Name:    "query",
						Aliases: []string{"q"},
						Usage:   "Path to the query CIDR list",
					},
				},
				Action: handleIntersect,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
