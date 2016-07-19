// Bulkdns takes a file with one domain name per line as input and will request your configured resolver(s) from /etc/resolv.conf for the nameservers of these domains.
// There are some command line arguments
//
// -v (--verbose) will print out some debug information and query results
//
// -c <int> number of concurrent queries
package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/miekg/dns"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// command line arguments
var verbose bool = false
var concurrent uint = 0

// list of resolvers to use
var resolvers = make([]string, 0)

const (
	TIMEOUT time.Duration = 5 // seconds
)

// translate rcode to human readable string
var rcode2string = map[int]string{
	0:  "Success",
	1:  "Format Error",
	2:  "Server Failure",
	3:  "Name Error",
	4:  "Not Implementd",
	5:  "Refused",
	6:  "YXDomain",
	7:  "YXRrset",
	8:  "NXRrset",
	9:  "Not Auth",
	10: "Not Zone",
	16: "Bad Signature / Bad Version",
	17: "Bad Key",
	18: "Bad Time",
	19: "Bad Mode",
	20: "Bad Name",
	21: "Bad Algorithm",
	22: "Bad Trunc",
	23: "Bad Cookie",
}

func main() {
	// define and parse command line arguments
	flag.BoolVar(&verbose, "verbose", false, "print more information while running")
	flag.BoolVar(&verbose, "v", false, "print more information while running")
     flag.UintVar(&concurrent, "concurrent", 1, "number of concurrent queries")
     flag.UintVar(&concurrent, "c", 1, "number of concurrent queries")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Printf("Usage: %s [-v] <filename>\n", os.Args[0])
		os.Exit(1)
	}

	initResolvers()

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		panic(err)
	}
	fastResolv(f)
	f.Close()
}

// initResolvers will read the list of resolvers from /etc/resolv.conf
func initResolvers() {
	conf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if conf == nil {
		fmt.Printf("Cannot initialize the local resolver: %s\n", err)
		os.Exit(1)
	}
	for i := range conf.Servers {
		server := conf.Servers[i]
		if strings.ContainsAny(":", server) {
			// IPv6 address
			server = "[" + server + "]:53"
		} else {
			server = server + ":53"
		}
		resolvers = append(resolvers, server)
		if verbose {
			fmt.Println("Found resolver " + server)
		}
	}
	if len(resolvers) == 0 {
		fmt.Println("No resolvers found.")
		os.Exit(5)
	}
}

// fastResolv will start a go routine to send a query. The number of go routines is limited.
func fastResolv(domains io.Reader) {
	var wg sync.WaitGroup
	var threads = make(chan string, concurrent)
	scanner := bufio.NewScanner(domains)
	server := 0

	for scanner.Scan() {
		wg.Add(1)
		threads <- "x"
		go resolv(scanner.Text(), resolvers[server], &wg, threads)
		server = (server + 1) % len(resolvers)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading domain list:", err)
	}
	wg.Wait()
	close(threads)
}

// resolv will send a query and return the result
func resolv(domain string, server string, wg *sync.WaitGroup, threads <-chan string) {
	if verbose {
		fmt.Printf("Resolving %s using %s\n", domain, server)
	}

     defer wg.Done()
     defer func () { _ = <-threads }()

	// make result list
	nslist := make([]string, 0)

	// Setting up query
	query := new(dns.Msg)
	query.RecursionDesired = true
	query.Question = make([]dns.Question, 1)
	query.SetQuestion(domain, dns.TypeNS)

	// Setting up resolver
	client := new(dns.Client)
	client.ReadTimeout = TIMEOUT * 1e9

	// make the query and wait for answer
	r, _, err := client.Exchange(query, server)

	// check for errors
	if err != nil {
		fmt.Printf("%-30s: Error resolving %s (server %s)\n", domain, err, server)
		return
	}
	if r == nil {
		fmt.Printf("%-30s: No answer (Server %s)\n", domain, server)
		return
	}
	if r.Rcode != dns.RcodeSuccess {
		fmt.Printf("%-30s: %s (Rcode %d, Server %s)\n", domain, rcode2string[r.Rcode], r.Rcode, server)
		return
	}

	// print out all NS
	if verbose {
		fmt.Printf("%-30s:", domain)
	}
	for _, answer := range r.Answer {
		if answer.Header().Rrtype == dns.TypeNS {
			nameserver := answer.(*dns.NS).Ns
			if verbose {
				fmt.Printf(" %s", nameserver)
			}
			nslist = append(nslist, nameserver)
		}
	}
	if verbose {
		fmt.Println("")
	}
}
