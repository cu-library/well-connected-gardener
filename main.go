package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// Verbose flag
	v = flag.Bool("v", false, "Verbose output")
	// A version flag, which should be overwritten when building using ldflags.
	version = "devel"
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Well Connected Gardener - Version %v\n", version)
		fmt.Fprintf(os.Stderr, "Enhance weeding lists by adding search results from other library OPACs.\n")
		fmt.Fprintf(os.Stderr, "usage: well-connected-gardener [-v] file [...]\n")
		fmt.Fprintf(os.Stderr, "flags:\n")
		flag.PrintDefaults()
	}
}

func errorLog(err error, reason string) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("%v - %v\n", err, reason)
	log.SetFlags(log.LstdFlags)
}

func process(ctx context.Context, filename string) {
	if *v {
		log.Printf("processing filename: %v\n", filename)
	}

	absPath, err := filepath.Abs(filename)
	if err != nil {
		errorLog(err, fmt.Sprintf("unable to get absolute path of %v.", filename))
		return
	}

	if *v {
		log.Printf("absolute path: %v\n", absPath)
	}

	file, err := os.Open(absPath)
	if err != nil {
		errorLog(err, "unable to open file for reading.")
		return
	}
	defer file.Close()

	r := csv.NewReader(file)
	r.Comma = '\t'
	r.LazyQuotes = true

	var header []string

ProcessingLoop:
	for {
		select {
		case <-ctx.Done():
			if *v {
				log.Printf("canceling processing of: %v\n", absPath)
			}
			break ProcessingLoop
		default:
		}

		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errorLog(err, fmt.Sprintf("unable to process file %v.", filename))
			break
		}

		if header == nil {
			lowercaserecord := record[:0]
			for _, x := range record {
				lowercaserecord = append(lowercaserecord, strings.TrimSpace(strings.ToLower(x)))
			}
			header = lowercaserecord
		} else {
			recordMap := map[string]string{}
			for i, label := range header {
				recordMap[label] = record[i]
			}
			fmt.Println(recordMap["020|a"])
			fmt.Printf("%#v\n", getISBNs(recordMap["020|a"]))
		}

	}
}

func getISBNs(raw020pipeA string) []string {
	isbns := []string{}
	// Split on the ";" delimiter
	for _, part := range strings.Split(strings.TrimSpace(raw020pipeA), "\";\"") {
		isbns = append(isbns, strings.Trim(strings.Split(part, " ")[0], ":."))
	}
	return isbns
}

func main() {

	// Parse the command line flags.
	flag.Parse()

	// Use this to ensure all files are processed
	// before exiting.
	var wg sync.WaitGroup

	// A context to pass to the file processing code
	// to allow for timeouts and canceling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Process each filename in the arguments.
	for _, filename := range flag.Args() {
		wg.Add(1)
		go func(filename string) {
			defer wg.Done()
			process(ctx, filename)
		}(filename)
	}

	// trap Ctrl+C and call cancel if received.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)
	go func() {
		select {
		case <-sigs:
			log.Println("Cancelling...")
			cancel()
			wg.Wait()
			log.Println("Done.")
		case <-ctx.Done():
		}
	}()

	// Wait for processing to complete.
	wg.Wait()
}
