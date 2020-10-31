package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/dgraph-io/badger/v2"
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

func (w Word) String() string {
	return fmt.Sprintf("{%s %d %d -> %.3f}", w.Text, w.Total, w.Spam, w.SpamLikelihood())
}

type Classifier struct {
	db *badger.DB

	spam  map[string]int // used during training, persisted in Close
	total map[string]int // see above

	thresholdUnsure float64
	thresholdSpam   float64
}

func NewClassifier(db *badger.DB, thresholdUnsure, thresholdSpam float64) Classifier {
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

func (c Classifier) persistDelta(label string, work chan delta) error {
	switch label {
	case "total", "spam":
	default:
		return errors.New("unexpected label: " + label)
	}

	// Loop: collect a bunch of deltas, persist them at once
	for first := range work {
		// Collect a bunch more
		deltas := []delta{first}
		for len(deltas) < 1024 {
			d, ok := <-work
			if !ok {
				// channel closed, finish remaining work
				break
			}

			deltas = append(deltas, d)
		}

		err := c.db.Update(func(tx *badger.Txn) (err error) {
			for _, delta := range deltas {
				key := []byte(label + "-" + delta.w)

				var v int

				current, err := tx.Get(key)
				switch err {
				case badger.ErrKeyNotFound:
					// Do nothing, assume current value is 0
				case nil:
					// Parse current value into v
					err := current.Value(func(d []byte) error {
						var err error
						v, err = strconv.Atoi(string(d))
						if err != nil {
							return errors.Wrap(err, "can't parse current value")
						}
						return nil
					})
					if err != nil {
						return err
					}
				default:
					return fmt.Errorf("getting current value for %q: %w", key, err)
				}

				v += delta.d
				// clamp value to 0 so things don't get weird
				if v < 0 {
					v = 0
				}

				err = tx.Set(key, []byte(strconv.Itoa(v)))
				if err != nil {
					return errors.Wrap(err, "updating value")
				}
			}

			return nil
		})

		if err != nil {
			panic(err)
		}
	}

	return nil
}

func (c Classifier) Persist(verbose bool) error {
	const concurrency = 8

	for _, label := range []string{"total", "spam"} {
		var wg sync.WaitGroup
		wg.Add(concurrency)

		deltas := make(chan delta)
		for idx := 0; idx < concurrency; idx++ {
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

		for word, diff := range source {
			deltas <- delta{
				w: word,
				d: diff,
			}
		}

		close(deltas)
		wg.Wait()
	}

	err := c.db.Sync()
	if err != nil {
		return errors.Wrap(err, "syncing db")
	}

	// Garbage collect DB
	cycles := 0
GC:
	for {
		err := c.db.RunValueLogGC(0.7)
		switch err {
		case badger.ErrNoRewrite:
			break GC
		case nil:
			cycles++
		default:
			return errors.Wrap(err, "running db gc")
		}
	}

	log.Println("ran", cycles, "complete GC cycles")

	err = c.db.View(func(tx *badger.Txn) error {
		it := tx.NewKeyIterator([]byte(""), badger.IteratorOptions{})
		defer it.Close()

		for it.Valid() {
			item := it.Item()
			log.Printf("item: %#v", item)
			it.Next()
		}

		return nil
	})

	if err != nil {
		panic(err)
	}

	return nil

}

func (c Classifier) Dump(out io.Writer) error {
	log.Println("starting dump")

	var words []Word

	err := c.db.View(func(tx *badger.Txn) error {
		it := tx.NewKeyIterator([]byte("total-"), badger.IteratorOptions{})
		defer it.Close()

		for it.Valid() {
			item := it.Item()

			word := strings.SplitN(string(item.Key()), "-", 2)[1]
			w, err := c.getWord(word)
			if err != nil {
				return fmt.Errorf("getting counters for %q: %w", word, err)
			}

			if w.Total > 5 {
				words = append(words, w)
			}

			it.Next()
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
		s1 := words[i].SpamLikelihood() * float64(words[i].Total)
		s2 := words[j].SpamLikelihood() * float64(words[j].Total)

		return s1 > s2
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

	err := c.db.View(func(tx *badger.Txn) error {
		total, err := tx.Get([]byte("total-" + word))
		switch err {
		case badger.ErrKeyNotFound:
		case nil:
			err = total.Value(func(v []byte) error {
				w.Total, err = strconv.Atoi(string(v))
				if err != nil {
					return errors.Wrap(err, "parsing total")
				}

				return nil
			})

			if err != nil {
				return errors.Wrap(err, "reading total")
			}
		default:
			return fmt.Errorf("getting total item for %q: %w", word, err)
		}

		spam, err := tx.Get([]byte("spam-" + word))
		switch err {
		case badger.ErrKeyNotFound:
		case nil:
			err = spam.Value(func(v []byte) error {
				w.Spam, err = strconv.Atoi(string(v))
				if err != nil {
					return errors.Wrap(err, "parsing spam")
				}

				return nil

			})

			if err != nil {
				return errors.Wrap(err, "reading spam")
			}
		default:
			return fmt.Errorf("getting spam item for %q: %w", word, err)
		}

		return nil
	})

	if err != nil {
		return w, err
	}

	return w, nil
}

// Train classifies the given word as spam or not spam, training c for future recognition.
func (c Classifier) Train(word string, spam bool, factor int) {
	c.total[word] += factor
	if spam {
		c.spam[word] += factor
	} else {
		c.spam[word] += factor
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
	scanner := bufio.NewScanner(NewFilteredReader(text))
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
