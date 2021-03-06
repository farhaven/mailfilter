package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

func (s *SpamFilter) trainingHandler(w http.ResponseWriter, r *http.Request) {
	// Params:
	// - learn as: spam/ham
	// - learn factor: int, how hard to learn
	// Read from r.Body, train, persist after training
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		code := http.StatusMethodNotAllowed
		http.Error(w, http.StatusText(code), code)
		return
	}

	args := r.URL.Query()

	trainAs := args.Get("as")
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

	start := time.Now()
	defer func() {
		log.Printf("training done as %q in %s, persisting", trainAs, time.Since(start))
	}()

	log.Println("factor:", learnFactor, "trainAs:", trainAs)

	err = s.c.Train(r.Body, trainAs == "spam", uint64(learnFactor))
	if err != nil {
		log.Printf("can't train message as %s: %s", trainAs, err)
		code := http.StatusInternalServerError
		http.Error(w, http.StatusText(code)+": "+err.Error(), code)
		return
	}

	fmt.Fprintln(w, "took", time.Since(start).String(), "to train", r.ContentLength, "bytes as", trainAs, "with factor", learnFactor)
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

	verbose := mode == ClassifyPlain && args.Get("verbose") == "true"

	err := s.classify(r.Body, w, mode, verbose)
	if err != nil {
		log.Println("can't classify message:", err)
		code := http.StatusInternalServerError
		http.Error(w, http.StatusText(code)+": "+err.Error(), code)
		return
	}
}

func (s *SpamFilter) handleIndex(w http.ResponseWriter, r *http.Request) {
	// TODO: Just expose Swagger endpoint
	code := http.StatusInternalServerError
	http.Error(w, http.StatusText(code), code)
}
