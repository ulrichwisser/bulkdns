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

var verbose bool = false
var fast bool = false
var concurrent uint = 0
var servers = make([]string, 0)

const (
	TIMEOUT time.Duration = 5 // seconds
)

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
	flag.BoolVar(&fast, "fast", false, "run many queries simultaniesly")
	flag.BoolVar(&fast, "f", false, "run many queries simultaniesly")
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
	if fast {
		fastResolv(f)
	} else {
		slowResolv(f)
	}
	f.Close()
}

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
		servers = append(servers, server)
		if verbose {
			fmt.Println("Found resolver " + server)
		}
	}
	if len(servers) == 0 {
		fmt.Println("No resolvers found.")
		os.Exit(5)
	}
}

func slowResolv(domains io.Reader) {
	scanner := bufio.NewScanner(domains)
	server := 0

	for scanner.Scan() {
		resolv(scanner.Text(), servers[server])
		server = (server + 1) % len(servers)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading domain list:", err)
	}
}

func fastResolv(domains io.Reader) {
	var wg sync.WaitGroup
	var threads = make(chan string, concurrent)
	scanner := bufio.NewScanner(domains)
	server := 0

	for scanner.Scan() {
		wg.Add(1)
		threads <- "x"
		go resolv2(scanner.Text(), servers[server], &wg, threads)
		server = (server + 1) % len(servers)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "reading domain list:", err)
	}
	wg.Wait()
	close(threads)
}

func resolv(domain string, server string) []string {
	if verbose {
		fmt.Printf("Resolving %s using %s\n", domain, server)
	}

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
		fmt.Printf("Error resolving %s from %s \n", domain, server, err)
		return nil
	}
	if r == nil {
		fmt.Printf("Error resolving %s from %s\n", domain, server)
		return nil
	}
	if r.Rcode != dns.RcodeSuccess {
		fmt.Printf("Error resolving %s from %s (Rcode %d %s)\n", domain, server, r.Rcode, rcode2string[r.Rcode])
		return nil
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
	return nslist
}

func resolv2(domain string, server string, wg *sync.WaitGroup, threads <-chan string) {
	resolv(domain, server)
	_ = <-threads
	wg.Done()
}
