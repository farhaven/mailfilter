// Mailfilter is a naive bayesian spam filter. It takes RFC2046-formatted mail on standard input
// and writes it to standard output, annotated with a spam score.
//
// For training, many messages can be concatenated and fed to standard input.
//
// Diagnostic messages will be written to stderr.
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/pkg/errors"

	"mailfilter/bloom"
	"mailfilter/classifier"
)

type SpamFilter struct {
	c *classifier.Classifier
}

type ClassifyMode int

const (
	ClassifyEmail ClassifyMode = iota
	ClassifyPlain
)

// classify reads a text from in, asks the given classifier to classify
// it as either spam or ham and writes it to out. The text is assumed to
// be a single RRC2046-encoded message, and the verdict is added as a
// header with the name `X-Mailfilter`.
func (s *SpamFilter) classify(in io.Reader, out io.Writer, how ClassifyMode, verbose bool) error {
	var msg bytes.Buffer

	start := time.Now()

	var (
		label classifier.Result
		err   error
	)

	if verbose {
		label, err = s.c.Classify(io.TeeReader(in, &msg), out)
	} else {
		label, err = s.c.Classify(io.TeeReader(in, &msg), nil)
	}
	if err != nil {
		return errors.Wrap(err, "classifying")
	}

	log.Printf("took %s to classify message as %s", time.Since(start), label)

	if how == ClassifyPlain {
		// Just write out the verdict to the output writer
		_, err := fmt.Fprintln(out, label)
		if err != nil {
			return errors.Wrap(err, "writing verdict")
		}

		return nil
	}

	log.Printf("got %d body bytes", msg.Len())

	// Write back message, inserting X-Mailfilter header at the bottom of the header block
	r := bufio.NewReader(&msg)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return errors.Wrap(err, "reading line")
		}

		if line == "\n" {
			// End of header block, insert verdict
			_, err = fmt.Fprintf(out, "X-Mailfilter: %s\n\n", label)
			if err != nil {
				return errors.Wrap(err, "writing verdict")
			}

			break
		}

		_, err = fmt.Fprint(out, line)
		if err != nil {
			return errors.Wrap(err, "writing header line")
		}
	}

	// Write rest of the mail
	_, err = io.Copy(out, r)
	if err != nil {
		return errors.Wrap(err, "writing body")
	}

	return nil
}

func main() {
	runtime.SetBlockProfileRate(20)
	runtime.SetMutexProfileFraction(20)

	user, err := user.Current()
	if err != nil {
		log.Fatalf("can't get current user: %s", err)
	}

	if user.HomeDir == "" {
		log.Fatalf("can't get home directory of user %#v", user)
	}

	listenAddr := flag.String("listenAddr", "127.0.0.1:7999", "Listening address for profiling server")
	dbPath := flag.String("dbPath", filepath.Join(user.HomeDir, ".flowers"), "path to word database")

	thresholdUnsure := flag.Float64("thresholdUnsure", 0.3, "Mail with score above this value will be classified as 'unsure'")
	thresholdSpam := flag.Float64("thresholdSpam", 0.7, "Mail with score above this value will be classified as 'spam'")

	flag.Parse()

	if *thresholdUnsure >= *thresholdSpam {
		fmt.Fprintf(flag.CommandLine.Output(), "Threshold for 'unknown' must be lower than threshold for 'spam'\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	log.Printf("thresholds: unsure=%f, spam=%f", *thresholdUnsure, *thresholdSpam)

	ctx, done := context.WithCancel(context.Background())
	defer done()

	dbTotal, err := bloom.NewDB(*dbPath, "total")
	if err != nil {
		log.Fatalf("can't open bloom db: %s", err)
	}

	dbSpam, err := bloom.NewDB(*dbPath, "spam")
	if err != nil {
		log.Fatalf("can't open bloom db: %s", err)
	}

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		dbTotal.Run(ctx)
	}()

	go func() {
		defer wg.Done()
		dbSpam.Run(ctx)
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		s := <-sigChan
		log.Println("got signal", s, "terminating")

		done()
	}()

	c := classifier.New(dbTotal, dbSpam, *thresholdUnsure, *thresholdSpam)

	s := SpamFilter{c}
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/train", s.trainingHandler)
	http.HandleFunc("/classify", s.classifyHandler)

	srv := http.Server{
		Addr: *listenAddr,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		<-ctx.Done()
		srv.Shutdown(ctx)
	}()

	log.Println("starting http server on", *listenAddr)
	err = srv.ListenAndServe()
	if err != nil {
		log.Printf("server terminated on %s: %s", *listenAddr, err)
	}

	wg.Wait()
}
