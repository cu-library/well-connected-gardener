package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"context"
	"time"
	"os/signal"
	"log"
	"path/filepath"
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
		fmt.Fprintln(os.Stderr, "Enhance weeding lists by adding search results from other library OPACs.\n")
		fmt.Fprintln(os.Stderr, "usage: well-connected-gardener [-v] file [...]\n")
		fmt.Fprintln(os.Stderr, "flags:")
		flag.PrintDefaults()
	}
}

func errorLog(err error, reason string){
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("%v - %v\n", err, reason)
	log.SetFlags(log.LstdFlags)
}

func processFile(ctx context.Context, filename string) {
	if *v {
		log.Printf("processing filename: %v\n", filename)
	}

	absPath, err := filepath.Abs(filename)
	if err != nil {
		errorLog(err, "unable to get absolute path of " + filename + ".")
		return
	}

	if *v {
		log.Printf("absolute path: %v\n", absPath)
	}

	file, err := os.Open(absPath)
	if err != nil {
		errorLog(err, "unable to open file for reading.")
	}
	defer file.Close()

	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		log.Println("timed out")
	}
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

	// Process each filename in the arguments.
	for _, filename := range flag.Args() {
		wg.Add(1)
		go func(filename string) {
			defer wg.Done()
			processFile(ctx, filename)
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
			log.Println("Done.")
		case <-ctx.Done():
		}
	}()

	// Wait for processing to complete.
	wg.Wait()
}
