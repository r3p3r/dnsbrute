package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chuhades/dnsbrute/dns"
	"github.com/chuhades/dnsbrute/log"
)

const versionNumber = "1.0#dev"

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: \n  %s [Options] {target}\n\nOptions\n", os.Args[0])
		flag.PrintDefaults()
	}

	debug := flag.Bool("debug", false, "Show debug information")
	requestDelay := flag.Duration("delay", time.Millisecond, "delay between each DNS request")
	threads := flag.Int("threads", 10, "number of DNS client(s)")
	dictFile := flag.String("dict", "dict/53683.txt", "dict file")
	target := flag.String("target", "", "target")
	version := flag.Bool("version", false, "Show program's version number and exit")
	flag.Parse()

	// show program's version number and exit
	if *version {
		fmt.Println(versionNumber)
		os.Exit(0)
	}

	if *target == "" {
		fmt.Println("no target")
		os.Exit(1)
	}

	// set log level to log.DEBUG
	if *debug {
		log.SetLevel(log.DEBUG)
	}

	// set delay between each DNS request
	dns.RequestDelay = *requestDelay

	// pan analytic
	log.Debug("generating blacklist of ip")
	dns.AnalyzePanAnalytic(*target)

	// query and records
	chQuery := make(chan string)
	queried := make(map[string]struct{})
	records := make(chan dns.DNSRecord)

	// query subdomains over dict and api
	go func() {
		ch := make(chan string)

		// query based on dict
		go func() {
			ch <- *target
			fd, err := os.Open(*dictFile)
			if err != nil {
				log.Fatal("Error while open dict:", err)
			}

			scanner := bufio.NewScanner(fd)
			for scanner.Scan() {
				sub := strings.TrimSpace(scanner.Text())
				if sub != "" {
					ch <- sub + "." + *target
				}
			}

			if err := scanner.Err(); err != nil {
				log.Fatal("Error while read dict:", err)
			}
			fd.Close()
		}()

		// query over api
		go func() {
			for domain := range dns.QueryOverAPI(*target) {
				ch <- domain
			}
		}()

		for domain := range ch {
			if _, ok := queried[domain]; !ok {
				queried[domain] = struct{}{}
				chQuery <- domain
			}
		}
	}()

	// clients
	wg := sync.WaitGroup{}
	clients := []dns.DNSClient{}
	for i := 0; i < *threads; i++ {
		clients = append(clients, dns.NewClient())
	}

	// drive all client
	for _, c := range clients {
		go func(client dns.DNSClient) {
			for domain := range chQuery {
				client.Query <- domain
			}
		}(c)
		wg.Add(1)
		go func(client dns.DNSClient) {
			defer wg.Done()
			for record := range client.Record {
				records <- record
			}
		}(c)
	}
	go func() {
		wg.Wait()
		close(records)
	}()

	for record := range records {
		log.Info(record)
	}
}
