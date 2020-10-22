package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
)

type Word struct {
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
	err := c.db.Batch(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return fmt.Errorf("getting %q bucket: %w", bucketName, err)
		}

		for delta := range deltas {
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
		log.Println("label", label)

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

			_, err = fmt.Fprintf(out, "%s\t%d\t%d\t%f\n", string(k), w.Total, w.Spam, w.SpamLikelihood())
			if err != nil {
				return errors.Wrap(err, "writing output line")
			}

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

	return nil
}

func (c Classifier) getWord(word string) (Word, error) {
	var w Word

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
	return fmt.Sprintf("label=%q, score=%.4f", c.Label, c.Score)
}

// Classify classifies the given text and returns a label along with a "certainty" value for that label.
func (c Classifier) Classify(text string) (ClassificationResult, error) {
	// TODO: This doesn't deal with float underflows. That's probably not good.
	//       Calculating this stuff in the log-domain doesn't seem to work though,
	//       or I'm too stupid. That seems more likely.

	words := strings.Fields(text)

	var scores []float64
	for _, w := range words {
		word, err := c.getWord(w)
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

	// score = a / (a + b)
	a := float64(1)
	b := float64(1)

	for _, s := range scores {
		a *= s
		b *= (1 - s)
	}

	result := ClassificationResult{
		Score: a / (a + b),
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
