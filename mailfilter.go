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
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"net/mail"
	"os"
	"os/user"
	"path/filepath"

	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
	"github.com/pkg/profile"
)

// writeMessage writes msg to out and returns a copy of out's body
func writeMessage(msg *mail.Message, out io.Writer) (io.Reader, error) {
	for hdr, vals := range msg.Header {
		for _, val := range vals {
			_, err := fmt.Fprintln(out, hdr+":", val)
			if err != nil {
				return nil, errors.Wrap(err, "writing header")
			}
		}
	}

	_, err := fmt.Fprintln(out, "")
	if err != nil {
		return nil, errors.Wrap(err, "writing header/body separator")
	}

	var buf bytes.Buffer

	_, err = io.Copy(out, io.TeeReader(msg.Body, &buf))
	if err != nil {
		return nil, errors.Wrap(err, "writing body")
	}

	return &buf, nil
}

// train reads text from in and trains the given classifier to recognize
// the text as ham or spam, depending on the spam flag.
func train(in io.Reader, c Classifier, spam, verbose bool) error {
	scanner := bufio.NewScanner(in)
	scanner.Split(ScanWords)

	words := 0
	for scanner.Scan() {
		word := scanner.Text()
		c.Train(word, spam)
		words++
	}

	if verbose {
		log.Println("trained", words, "words", spam)
	}

	return nil
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
func classify(in io.Reader, c Classifier, out io.Writer, how ClassifyMode) error {
	var (
		buf bytes.Buffer
		msg bytes.Buffer
	)

	_, err := io.Copy(&buf, io.TeeReader(in, &msg))
	if err != nil {
		return errors.Wrap(err, "reading into temp. buffer")
	}

	label, err := c.Classify(&buf)
	if err != nil {
		return errors.Wrap(err, "classifying")
	}

	if how == ClassifyPlain {
		// Just write out the verdict to the output writer
		_, err := fmt.Fprintln(out, label)
		if err != nil {
			return errors.Wrap(err, "writing verdict")
		}

		return nil
	}

	// Write back message, inserting X-Mailfilter header at the bottom of the header block
	r := bufio.NewReader(&msg)
	for {
		line, err := r.ReadString('\n')
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
	user, err := user.Current()
	if err != nil {
		log.Fatalf("can't get current user: %s", err)
	}

	if user.HomeDir == "" {
		log.Fatalf("can't get home directory of user %#v", user)
	}

	dump := flag.Bool("dump", false, "dump frequency data to stdout")
	verbose := flag.Bool("verbose", false, "be more verbose during training")
	profilingAddr := flag.String("profilingAddr", "127.0.0.1:7999", "Listening address for profiling server")
	dbPath := flag.String("dbPath", filepath.Join(user.HomeDir, ".mailfilter.db"), "path to word database")

	doTrain := flag.String("train", "", "How to train this message. If not provided, no training is done. One of [ham,spam] otherwise")

	doClassify := flag.String("classify", "email", "How to classify this message. If empty, no classification is done. One of [email, plain]")
	thresholdUnsure := flag.Float64("thresholdUnsure", 0.3, "Mail with score above this value will be classified as 'unsure'")
	thresholdSpam := flag.Float64("thresholdSpam", 0.7, "Mail with score above this value will be classified as 'spam'")

	flag.Parse()

	go func() {
		log.Println("starting profiling server on", *profilingAddr)
		err := http.ListenAndServe(*profilingAddr, nil)
		if err != nil {
			log.Printf("can't start profiling server on %s: %s", *profilingAddr, err)
		}
	}()

	if *profilingAddr == "" {
		defer profile.Start(profile.ProfilePath("/tmp")).Stop()
	}

	switch *doTrain {
	case "", "ham", "spam":
	default:
		fmt.Fprintf(flag.CommandLine.Output(), "Don't know how to train %q\n\n", *doTrain)
		flag.PrintDefaults()
		os.Exit(1)
	}

	switch *doClassify {
	case "", "email", "plain":
	default:
		fmt.Fprintf(flag.CommandLine.Output(), "Don't know how to classify %q\n\n", *doClassify)
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *thresholdUnsure >= *thresholdSpam {
		fmt.Fprintf(flag.CommandLine.Output(), "Threshold for 'unknown' must be lower than threshold for 'spam'\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	db, err := bolt.Open(*dbPath, 0600, &bolt.Options{})
	if err != nil {
		log.Fatalf("can't open database: %s", err)
	}
	defer db.Close()

	log.Println("database open")

	c := NewClassifier(db, *thresholdUnsure, *thresholdSpam)

	if *doTrain != "" {
		defer func() {
			err := c.Persist(*verbose)
			if err != nil {
				log.Panicf("can't persist db: %s", err)
			}

			log.Println("done")
		}()

		err = train(os.Stdin, c, *doTrain == "spam", *verbose)
		if err != nil {
			log.Fatalf("can't train message as %s: %s", *doTrain, err)
		}

		*doClassify = ""
	}

	if *doClassify != "" {
		mode := ClassifyEmail
		if *doClassify == "plain" {
			mode = ClassifyPlain
		}

		err := classify(os.Stdin, c, os.Stdout, mode)
		if err != nil {
			log.Fatalf("can't classify message: %s", err)
		}
	}

	if *dump {
		err := c.Dump(os.Stdout)
		if err != nil {
			log.Fatalf("can't dump word frequencies: %s", err)
		}
	}
}
