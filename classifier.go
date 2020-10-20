package main

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

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

	thresholdUnsure float64
	thresholdSpam   float64
}

func NewClassifier(db *bolt.DB, thresholdUnsure, thresholdSpam float64) Classifier {
	return Classifier{
		db:              db,
		thresholdUnsure: thresholdUnsure,
		thresholdSpam:   thresholdSpam,
	}
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
func (c Classifier) Train(word string, spam bool) error {
	// Increase total and (maybe) spam count for word
	err := c.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("total"))
		if err != nil {
			return errors.Wrap(err, "getting 'total' bucket")
		}

		w := []byte(word)

		var v int

		d := b.Get(w)
		if len(d) != 0 {
			v, err = strconv.Atoi(string(d))
			if err != nil {
				return errors.Wrap(err, "parsing total")
			}
		}

		err = b.Put(w, []byte(strconv.Itoa(v+1)))
		if err != nil {
			return errors.Wrap(err, "writing total value")
		}

		if !spam {
			return nil
		}

		// Record word as spam
		b, err = tx.CreateBucketIfNotExists([]byte("spam"))
		if err != nil {
			return errors.Wrap(err, "getting 'spam' bucket")
		}

		v = 0
		d = b.Get(w)
		if len(d) != 0 {
			v, err = strconv.Atoi(string(d))
			if err != nil {
				return errors.Wrap(err, "parsing spam count")
			}
		}

		err = b.Put(w, []byte(strconv.Itoa(v+1)))
		if err != nil {
			return errors.Wrap(err, "writing spam value")
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("training %q (spam:%t): %w", word, spam, err)
	}

	return nil
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
	}

	if result.Score > c.thresholdUnsure {
		result.Label = "unsure"
	}

	if result.Score > c.thresholdSpam {
		result.Label = "spam"
	}

	return result, nil
}
