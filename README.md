# cidrq
CLI multi-tool for lists of IPs and CIDRs

## Use cases
With cidrq, you can...

* **Filter** - filter CIDRs using match lists and exclusion lists
* **Combine** - calculate unions, intersections and differences
* **Validate and sanitize** - extract IPs from URLs; scan for lines that contain (or don't contain) valid IPs/CIDRs

## Installation
```
go install github.com/aromatt/cidrq@latest
```
## Documentation
### Overview
```
NAME:
   cidrq - CIDR manipulation tool

USAGE:
   cidrq [global options] command [command options]

AUTHOR:
   Andrew Matteson <andrew.k.matteson@gmail.com>

COMMANDS:
   combine, c  Combine lists of CIDRs
   sort, s     Sort lists of CIDRs
   filter, f   Filter lists of CIDRs
   help, h     Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --err value, -e value Action to take on error (abort, skip, warn, print). In
      print mode, the input line is printed to stdout. Default: abort. (default:
      "abort")
   --verbose, -v  Print verbose logs to stderr. Default: false. (default: false)
   --help, -h     show help
```
### Subcommands
#### Combine
```
NAME
      cidrq combine - Combine lists of CIDRs

USAGE
      cidrq combine [command options] [paths]

DESCRIPTION
      Provide files containing lists of CIDRs using --union (-u), --intersect
      (-i), or --subtract (-s). Operations are performed in the order they are
      provided, with stdin as the first list. The output is a compacted list of
      CIDRs.

OPTIONS
      --union FILE, -u FILE
            Return the union of the working set with the CIDRs in `FILE`.

      --intersect FILE, -i FILE
            Return the intersection of the working set with the CIDRs in `FILE`.

      --subtract FILE, -s FILE
            Subtract the CIDRs in `FILE` from the working set. If a subtracted
            CIDR is a child of a working-set CIDR, then the working-set CIDR will
            be split, leaving behind the remaining portion.

      --help, -h
            show help

```
#### Sort
```
NAME
      cidrq sort - Sort lists of CIDRs

USAGE
      cidrq sort [command options] [paths]

OPTIONS
      --help, -h
            show help

```
#### Filter
```

NAME
      cidrq filter - Filter lists of CIDRs

USAGE
      cidrq filter [command options] [paths]

OPTIONS
      --exclude FILE, -x FILE
            Path to CIDR exclusion list. Input lines containing CIDRs found in
            `FILE` will be omitted from the output.

      --exclude-mode value
            Comparison strategy for exclude list (overlap, encompass). In overlap
            mode, an input CIDR is excluded if it overlaps any CIDR in an exclude
            list. In encompass mode, an input CIDR is only excluded if it has a
            parent in an exclude list. Default: encompass.

      --match FILE, -m FILE
            Path to CIDR match list. The filter will permit any input lines
            containing CIDRs that match any of the CIDRs in `FILE`. If -exclude is
            provided, it will be applied after matching.

      --match-mode value
            Comparison strategy for match list (overlap, encompass). In overlap
            mode, an input CIDR is a match if it overlaps any CIDR in a match
            list. In encompass mode, an input CIDR matches only if it has a parent
            in a match list. Default: overlap.

      --field value, -f value
            Instruct cidrq to look for CIDRs in one or more fields, where field
            delimiter is provided via -d. Parsing is performed only on input
            CIDRs, not exclusion or match lists.

      --delimiter value, -d value
            Delimiter for field separation (use '\t' for tab).

      --quiet, -q
            Suppress stdout. If -err == print, error lines are still printed.

      --clean, -c
            Replace selected fields with their respective parsed CIDRs

      --url, -u
            Accept a URL as valid if the hostname is a valid IP.

      --host, -H
            Accept a host[:port] as valid if the host is a valid IP.

      --flat, -F
            Print each matched CIDR on a separate line.

      --help, -h
            show help
```
