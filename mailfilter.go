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
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/pkg/errors"

	"mailfilter/db"
)

type SpamFilter struct {
	c *Classifier
}

// train reads text from in and trains the given classifier to recognize
// the text as ham or spam, depending on the spam flag.
func (s *SpamFilter) train(in io.Reader, spam bool, learnFactor int) error {
	words := make(chan string, 8192)

	go func() {
		defer close(words)

		scanner := bufio.NewScanner(NewFilteredReader(in))
		scanner.Split(ScanNGram)

		for scanner.Scan() {
			word := scanner.Text()
			words <- word
		}
	}()

	for word := range words {
		s.c.Train(word, spam, learnFactor)
	}

	return nil
}

func (s *SpamFilter) trainingHandler(w http.ResponseWriter, r *http.Request) {
	// Params:
	// - learn as: spam/ham
	// - learn factor: int, how hard to learn
	// Read from r.Body, train, persist after training

	defer r.Body.Close()

	defer func() {
		log.Println("training done, persisting")

		err := s.c.Persist()
		if err != nil {
			log.Panicf("can't persist db: %s", err)
		}
	}()

	args := r.URL.Query()

	trainAs := args.Get("train")
	if trainAs == "" {
		trainAs = "spam"
	}

	switch trainAs {
	case "spam", "ham":
	default:
		panic(trainAs) // TODO: Handle properly
	}

	learnFactorArg := args.Get("factor")
	if learnFactorArg == "" {
		learnFactorArg = "1"
	}
	learnFactor, err := strconv.Atoi(learnFactorArg)
	if err != nil {
		panic(err) // TODO: Handle properly
	}

	err = s.train(r.Body, trainAs == "spam", learnFactor)
	if err != nil {
		// TODO: Properly handle this
		log.Fatalf("can't train message as %s: %s", trainAs, err)
	}

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
func (s *SpamFilter) classify(in io.Reader, out io.Writer, how ClassifyMode) error {
	var msg bytes.Buffer

	start := time.Now()
	label, err := s.c.Classify(io.TeeReader(in, &msg))
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

	log.Printf("got %d bytes: %s", msg.Len(), msg.String())

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

func (s *SpamFilter) classifyHandler(w http.ResponseWriter, r *http.Request) {
	// Params: type of classification: Plain or Email
	// Read from r.Body, write to w
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		code := http.StatusMethodNotAllowed
		http.Error(w, http.StatusText(code), code)
		return
	}

	args := r.URL.Query()

	var mode ClassifyMode
	switch args.Get("mode") {
	case "", "email":
		mode = ClassifyEmail
	case "plain":
		mode = ClassifyPlain
	default:
		http.Error(w, fmt.Sprintf("unexpected mode %q", args.Get("mode")), http.StatusBadRequest)
		return
	}

	err := s.classify(r.Body, w, mode)
	if err != nil {
		// TODO: Proper error handling
		log.Fatalf("can't classify message: %s", err)
	}
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
	dbPath := flag.String("dbPath", filepath.Join(user.HomeDir, ".mailfilter.db"), "path to word database")

	thresholdUnsure := flag.Float64("thresholdUnsure", 0.3, "Mail with score above this value will be classified as 'unsure'")
	thresholdSpam := flag.Float64("thresholdSpam", 0.7, "Mail with score above this value will be classified as 'spam'")

	flag.Parse()

	startTime := time.Now()

	defer func() {
		log.Println("done in", time.Since(startTime))
	}()

	if *thresholdUnsure >= *thresholdSpam {
		fmt.Fprintf(flag.CommandLine.Output(), "Threshold for 'unknown' must be lower than threshold for 'spam'\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	db, err := db.Open(*dbPath, true)
	if err != nil {
		log.Fatalf("can't open database: %s", err)
	}
	defer db.LogStats()
	defer db.Close()

	c := NewClassifier(db, *thresholdUnsure, *thresholdSpam)

	s := SpamFilter{c}
	http.HandleFunc("/train", s.trainingHandler)
	http.HandleFunc("/classify", s.classifyHandler)

	log.Println("starting http server on", *listenAddr)
	err = http.ListenAndServe(*listenAddr, nil)
	if err != nil {
		log.Printf("can't start profiling server on %s: %s", *listenAddr, err)
	}

}
