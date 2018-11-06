package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// Verbose flag
	v = flag.Bool("v", false, "Verbose output")
	// A version flag, which should be overwritten when building using ldflags.
	version = "devel"
)

const YazTemplateISBNUofT string = `open sirsi.library.utoronto.ca:2200
find @attr 1=7 "%v"
quit
`

const YazTemplateISBNUofO string = `open orbis.uottawa.ca:210/INNOPAC
find @attr 1=7 "%v"
close
quit
`

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Well Connected Gardener - Version %v\n", version)
		fmt.Fprintf(os.Stderr, "Enhance weeding lists by adding search results from other library OPACs.\n")
		fmt.Fprintf(os.Stderr, "usage: well-connected-gardener [-v] file [...]\n")
		fmt.Fprintf(os.Stderr, "flags:\n")
		flag.PrintDefaults()
	}
}

func process(ctx context.Context, filename string) {
	if *v {
		log.Printf("processing filename: %v\n", filename)
	}

	absPath, err := filepath.Abs(filename)
	if err != nil {
		log.Printf("%v - unable to get absolute path of %v.\n", err, filename)
		return
	}

	if *v {
		log.Printf("absolute path: %v\n", absPath)
	}

	file, err := os.Open(absPath)
	if err != nil {
		log.Printf("%v - unable to open file for reading.", err)
		return
	}
	defer file.Close()

	dir := filepath.Dir(absPath)
	ext := filepath.Ext(absPath)
	base := filepath.Base(absPath)
	modified := filepath.Join(dir, strings.TrimSuffix(base, ext)+"_augmented"+ext)

	output, err := os.Create(modified)
	if err != nil {
		log.Printf("%v - unable to open file for writing.", err)
		return
	}
	defer output.Close()

	r := csv.NewReader(file)
	r.Comma = '\t'
	r.LazyQuotes = true

	o := csv.NewWriter(output)
	o.Comma = '\t'

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
			log.Printf("%v - unable to process file %v.", err, filename)
			return
		}

		if header == nil {
			newHeader := append([]string{}, record...)
			newHeader = append(newHeader, "FOUND IN UOFO CATALOGUE")
			newHeader = append(newHeader, "FOUND IN UOFT CATALOGUE")
			o.Write(newHeader)

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

			if *v {
				log.Printf("%#v\n", recordMap)
			}

			foundInUofOCat := false
			foundInUofTCat := false

			for _, isbn := range getISBNs(recordMap["020|a"]) {

				if *v {
					log.Printf("ISBN: %v\n", isbn)
				}

				uoforesult, err := z3950forISBN(isbn, YazTemplateISBNUofO)
				if err != nil {
					log.Println(err)
					break ProcessingLoop
				}
				if uoforesult {
					foundInUofOCat = true
				}
				if *v {
					log.Printf("UofO Result: %v\n", uoforesult)
				}

				uoftresult, err := z3950forISBN(isbn, YazTemplateISBNUofT)
				if err != nil {
					log.Println(err)
					break ProcessingLoop
				}
				if uoftresult {
					foundInUofTCat = true
				}
				if *v {
					log.Printf("UofT Result: %v\n", uoftresult)
				}

				time.Sleep(500 * time.Millisecond)
			}

			newRecord := append([]string{}, record...)
			newRecord = append(newRecord, strconv.FormatBool(foundInUofOCat))
			newRecord = append(newRecord, strconv.FormatBool(foundInUofTCat))
			o.Write(newRecord)
		}

		// Write any buffered data to the underlying writer (standard output).
		o.Flush()

		if err := o.Error(); err != nil {
			log.Printf("%v - unable to flush csv file %v.", err, modified)
			break ProcessingLoop
		}
	}
}

func getISBNs(raw020pipeA string) []string {
	isbns := []string{}
	// Split on the ";" delimiter
	for _, part := range strings.Split(strings.TrimSpace(raw020pipeA), "\";\"") {
		isbn := strings.Trim(strings.Split(part, " ")[0], ":.")
		if isbn != "" {
			isbns = append(isbns, isbn)
		}
	}
	return isbns
}

func main() {

	// Parse the command line flags.
	flag.Parse()

	if len(flag.Args()) == 0 {
		log.Fatalln("Please provide one file to process.")
	}

	// Check to see if we have yaz-client available to us.
	out, err := exec.Command("yaz-client", "-V").Output()
	if err != nil {
		log.Fatalf("Unable to execute yaz-client: %v\n", err)
	}
	if *v {
		log.Printf("yaz-client -V\n")
		log.Printf("%s", out)
	}
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

func z3950forISBN(isbn string, template string) (bool, error) {

	found := false

	// The command to execute
	cmd := exec.Command("yaz-client", "-f", "/dev/stdin")
	//cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Println("unable to create new StdinPipe")
		return found, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Println("unable to create new StdoutPipe")
		return found, err
	}

	if err := cmd.Start(); err != nil {
		log.Println("error starting exec'd process")
		return found, err
	}

	io.WriteString(stdin, fmt.Sprintf(template, isbn))

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Number of hits:") {
			count, err := strconv.Atoi(strings.TrimSuffix(strings.Fields(line)[3], ","))
			if err == nil && count > 0 {
				found = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Println("error scanning from exec'd process")
		return found, err
	}

	if err := cmd.Wait(); err != nil {
		log.Println("error waiting for exec'd command to complete")
		return found, err
	}

	return found, nil
}
