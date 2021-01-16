package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
)

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
