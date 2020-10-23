package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
)

func ScanNGram(data []byte, atEOF bool) (advance int, token []byte, err error) {
	const maxLength = 16

	// Skip leading spaces.
	start := 0
	for width := 0; start < len(data); start += width {
		var r rune
		r, width = utf8.DecodeRune(data[start:])
		if !unicode.IsSpace(r) {
			break
		}
	}
	// Scan until space, marking end of word.
	for width, i := 0, start; i < len(data); i += width {
		var r rune
		r, width = utf8.DecodeRune(data[i:])
		if unicode.IsSpace(r) {
			return i + width, data[start:i], nil
		}

		if i >= maxLength {
			return i + width - 1, data[start:i], nil
		}
	}

	// If we're at EOF, we have a final, non-empty, non-terminated word. Return it.
	if atEOF && len(data) > start {
		return len(data), data[start:], nil
	}
	// Request more data.
	return start, nil, nil
}

var (
	exprPunct  = regexp.MustCompile(`[\p{P}\p{S}\p{C}\p{M}]+`)
	exprNumber = regexp.MustCompile(`[\p{N}]+`)
	exprSep    = regexp.MustCompile(`[\p{Z}]+`)
)

type FilteredReader struct {
	r io.Reader
}

func (r FilteredReader) Read(data []byte) (int, error) {
	n, err := r.r.Read(data)
	if err != nil {
		return 0, err
	}

	b := bytes.ToLower(data[:n])

	b = exprPunct.ReplaceAll(b, []byte("!"))
	b = exprNumber.ReplaceAll(b, []byte("#"))
	b = exprSep.ReplaceAll(b, []byte(" "))

	copy(data, b)

	return len(b), nil
}

type Word struct {
	Text  string
	Total int
	Spam  int
}

func (w Word) SpamLikelihood() float64 {
	if w.Total == 0 {
		// haven't seen this word yet, can't say anything about it.
		return 0.5
	}

	score := float64(w.Spam) / float64(w.Total)

	if math.IsInf(score, 0) {
		panic(fmt.Sprintf("infinite score for %v", w))
	}

	if math.IsNaN(score) {
		panic(fmt.Sprintf("nan score for %v", w))
	}

	return score
}

type Classifier struct {
	db *bolt.DB

	spam  map[string]int // used during training, persisted in Close
	total map[string]int // see above

	thresholdUnsure float64
	thresholdSpam   float64
}

func NewClassifier(db *bolt.DB, thresholdUnsure, thresholdSpam float64) Classifier {
	return Classifier{
		db: db,

		spam:  make(map[string]int),
		total: make(map[string]int),

		thresholdUnsure: thresholdUnsure,
		thresholdSpam:   thresholdSpam,
	}
}

type delta struct {
	w string
	d int
}

func (c Classifier) persistDelta(bucketName string, deltas chan delta) error {
	err := c.db.Update(func(tx *bolt.Tx) error {
		for delta := range deltas {
			bucket, err := tx.CreateBucketIfNotExists([]byte(bucketName))
			if err != nil {
				return fmt.Errorf("getting %q bucket: %w", bucketName, err)
			}

			word := []byte(delta.w)

			var v int

			d := bucket.Get(word)
			if len(d) != 0 {
				v, err = strconv.Atoi(string(d))
				if err != nil {
					return errors.Wrap(err, "parsing total")
				}
			}

			err = bucket.Put(word, []byte(strconv.Itoa(v+delta.d)))
			if err != nil {
				return errors.Wrap(err, "writing total value")
			}
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "persisting delta")
	}

	return nil
}

func (c Classifier) Persist(verbose bool) error {
	if verbose {
		log.Println("persisting updated training data", len(c.spam), len(c.total))

		err := c.db.View(func(tx *bolt.Tx) error {
			totalBucket := tx.Bucket([]byte("total"))
			if totalBucket != nil {
				log.Printf("> Total bucket: %#v", totalBucket.Stats())
			}

			spamBucket := tx.Bucket([]byte("spam"))
			if spamBucket != nil {
				log.Printf("> Spam bucket:  %#v", spamBucket.Stats())
			}

			return nil
		})

		if err != nil {
			return errors.Wrap(err, "getting bucket stats")
		}
	}

	const concurrency = 8

	for _, label := range []string{"total", "spam"} {
		var wg sync.WaitGroup
		wg.Add(concurrency)

		deltas := make(chan delta)
		for idx := 0; idx < concurrency; idx++ {
			if verbose {
				log.Println("starting persister", idx)
			}

			go func() {
				defer wg.Done()
				err := c.persistDelta(label, deltas)
				if err != nil {
					log.Panicf("failed to persist %s: %s", label, err)
				}
			}()
		}

		source := c.total
		if label == "spam" {
			source = c.spam
		}

		if verbose {
			log.Println("label", label, "with", len(source), "delta entries")
		}

		for word, diff := range source {
			deltas <- delta{
				w: word,
				d: diff,
			}
		}

		close(deltas)
		wg.Wait()
	}

	if verbose {
		err := c.db.View(func(tx *bolt.Tx) error {
			totalBucket := tx.Bucket([]byte("total"))
			if totalBucket != nil {
				log.Printf("< Total bucket: %#v", totalBucket.Stats())
			}

			spamBucket := tx.Bucket([]byte("spam"))
			if spamBucket != nil {
				log.Printf("< Spam bucket:  %#v", spamBucket.Stats())
			}

			return nil
		})

		if err != nil {
			return errors.Wrap(err, "getting bucket stats")
		}
	}

	return nil

}

func (c Classifier) Dump(out io.Writer) error {
	log.Println("starting dump")

	var words []Word

	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("total"))
		if b == nil {
			return nil
		}

		err := b.ForEach(func(k, v []byte) error {
			w, err := c.getWord(string(k))
			if err != nil {
				return fmt.Errorf("getting counters for %q: %w", k, err)
			}

			words = append(words, w)

			return nil
		})

		if err != nil {
			return errors.Wrap(err, "iterating over 'total' bucket")
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "dumping database")
	}

	// Dump top 25 total words, and top 25 spam words
	sort.Slice(words, func(i, j int) bool {
		return words[i].Total > words[j].Total
	})

	_, err = fmt.Fprintln(out, "Top 25 words:")
	if err != nil {
		return errors.Wrap(err, "writing total header")
	}

	for idx := 0; idx < 25 && idx < len(words)-1; idx++ {
		_, err = fmt.Fprintln(out, idx, words[idx])
		if err != nil {
			return errors.Wrap(err, "writing total entry "+strconv.Itoa(idx))
		}
	}

	sort.Slice(words, func(i, j int) bool {
		return words[i].SpamLikelihood() > words[j].SpamLikelihood()
	})

	_, err = fmt.Fprintln(out, "Top 25 spam words:")
	if err != nil {
		return errors.Wrap(err, "writing spam header")
	}

	for idx := 0; idx < 25 && idx < len(words)-1; idx++ {
		_, err = fmt.Fprintln(out, idx, words[idx])
		if err != nil {
			return errors.Wrap(err, "writing spam entry "+strconv.Itoa(idx))
		}
	}

	return nil
}

func (c Classifier) getWord(word string) (Word, error) {
	w := Word{
		Text: word,
	}

	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("total"))
		if b == nil {
			return nil
		}

		d := b.Get([]byte(word))
		if len(d) == 0 {
			return nil
		}

		var err error
		w.Total, err = strconv.Atoi(string(d))
		if err != nil {
			return errors.Wrap(err, "reading total")
		}

		b = tx.Bucket([]byte("spam"))
		if b == nil {
			return nil
		}

		d = b.Get([]byte(word))
		if len(d) == 0 {
			return nil
		}

		w.Spam, err = strconv.Atoi(string(d))

		return nil
	})

	if err != nil {
		return w, err
	}

	return w, nil
}

// Train classifies the given word as spam or not spam, training c for future recognition.
func (c Classifier) Train(word string, spam bool) {
	c.total[word]++
	if spam {
		c.spam[word]++
	}
}

func sigmoid(x float64) float64 {
	if x < 0 || x > 1 {
		panic(fmt.Sprintf("x out of [0, 1]: %f", x))
	}

	midpoint := 0.5
	max := 0.999999
	k := 20.0

	return max / (1.0 + math.Exp(-k*(x-midpoint)))
}

type ClassificationResult struct {
	Label string
	Score float64
}

func (c ClassificationResult) String() string {
	return fmt.Sprintf("label=%q, score=%.6f", c.Label, c.Score)
}

// Classify classifies the given text and returns a label along with a "certainty" value for that label.
func (c Classifier) Classify(text io.Reader) (ClassificationResult, error) {
	scanner := bufio.NewScanner(FilteredReader{text})
	scanner.Split(ScanNGram)

	var scores []float64
	for scanner.Scan() {
		word, err := c.getWord(scanner.Text())
		if err != nil {
			return ClassificationResult{}, errors.Wrap(err, "getting word counts")
		}

		p := word.SpamLikelihood()

		// Pass scores through a tuned sigmoid so that they stay strictly above 0 and
		// strictly below 1. This makes calculating with the inverse a bit easier, at
		// the expense of never returning an absolute verdict, and slightly biasing
		// towards detecting stuff as ham, since sigmoid(0.5) < 0.5.
		s := sigmoid(p)

		scores = append(scores, s)
	}

	eta := float64(0)

	for _, p := range scores {
		l1 := math.Log(1 - p)
		l2 := math.Log(p)

		if math.IsNaN(l1) || math.IsInf(l1, 0) {
			panic(fmt.Sprintf("l1: %f", l1))
		}

		if math.IsNaN(l2) || math.IsInf(l2, 0) {
			panic(fmt.Sprintf("l2: %f", l2))
		}

		eta += l1 - l2

		if math.IsNaN(eta) || math.IsInf(eta, 0) {
			panic(fmt.Sprintf("eta: %f", eta))
		}
	}

	score := 1.0 / (1.0 + math.Exp(eta))
	if math.IsNaN(score) || math.IsInf(score, 0) {
		panic(fmt.Sprintf("score: %f", score))
	}

	result := ClassificationResult{
		Score: score,
		Label: "ham",
	}

	if result.Score > c.thresholdUnsure {
		result.Label = "unsure"
	}

	if result.Score > c.thresholdSpam {
		result.Label = "spam"
	}

	return result, nil
}
