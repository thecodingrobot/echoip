package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/mpolden/echoip/http"
	"github.com/mpolden/echoip/iputil"
	"github.com/mpolden/echoip/iputil/geo"
)

type multiValueFlag []string

func (f *multiValueFlag) String() string {
	return strings.Join([]string(*f), ", ")
}

func (f *multiValueFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func init() {
	log.SetPrefix("echoip: ")
	log.SetFlags(log.Lshortfile)
}

func main() {
	countryFile := flag.String("f", "", "Path to GeoIP country database")
	cityFile := flag.String("c", "", "Path to GeoIP city database")
	asnFile := flag.String("a", "", "Path to GeoIP ASN database")
	listen := flag.String("l", ":8080", "Listening address")
	reverseLookup := flag.Bool("r", false, "Perform reverse hostname lookups")
	portLookup := flag.Bool("p", false, "Enable port lookup")
	template := flag.String("t", "html", "Path to template dir")
	cacheSize := flag.Int("C", 0, "Size of response cache. Set to 0 to disable")
	profileAddr := flag.String("P", "", "Listening address for debug/profiling handlers (e.g. 127.0.0.1:6060). Empty disables them. NEVER expose this to the public internet: it serves pprof (which can pin a CPU for 30s) and a cache resize POST endpoint.")
	sponsor := flag.Bool("s", false, "Show sponsor logo")
	var headers multiValueFlag
	flag.Var(&headers, "H", "Header to trust for remote IP, if present (e.g. X-Real-IP)")
	flag.Parse()
	if len(flag.Args()) != 0 {
		flag.Usage()
		os.Exit(2)
	}

	r, err := geo.Open(*countryFile, *cityFile, *asnFile)
	if err != nil {
		log.Fatal(err)
	}
	cache := http.NewCache(*cacheSize)
	server := http.New(r, cache)
	server.IPHeaders = headers
	if _, err := os.Stat(*template); err == nil {
		server.Template = *template
		if *template != "" {
			if err := server.LoadTemplates(); err != nil {
				log.Fatal(err)
			}
		}
	} else {
		log.Printf("Not configuring default handler: Template not found: %s", *template)
	}
	if *reverseLookup {
		log.Println("Enabling reverse lookup")
		server.LookupAddr = iputil.LookupAddr
	}
	if *portLookup {
		log.Println("Enabling port lookup")
		server.LookupPort = iputil.LookupPort
	}
	if *sponsor {
		log.Println("Enabling sponsor logo")
		server.Sponsor = *sponsor
	}
	if len(headers) > 0 {
		log.Printf("Trusting remote IP from header(s): %s", headers.String())
	}
	if *cacheSize > 0 {
		log.Printf("Cache capacity set to %d", *cacheSize)
	}

	// signal.NotifyContext cancels ctx on SIGINT/SIGTERM, triggering
	// graceful shutdown of all running listeners.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	if *profileAddr != "" {
		log.Printf("Enabling debug/profiling handlers on http://%s (do not expose publicly)", *profileAddr)
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			if err := server.ListenAndServeDebug(ctx, addr); err != nil {
				errCh <- err
				stop() // trigger shutdown of the public listener too
			}
		}(*profileAddr)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Listening on http://%s", *listen)
		if err := server.ListenAndServe(ctx, *listen); err != nil {
			errCh <- err
			stop()
		}
	}()

	<-ctx.Done()
	log.Println("Shutdown signal received, draining in-flight requests...")
	wg.Wait()
	close(errCh)

	var failed bool
	for err := range errCh {
		log.Printf("listener error: %s", err)
		failed = true
	}
	if failed {
		os.Exit(1)
	}
	log.Println("Shutdown complete")
}
