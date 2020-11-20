package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"unicode"
	"unicode/utf8"

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

type DB interface {
	Get(bucket string, key string) int
	Inc(bucket string, key string, delta int, clamp bool) error
}

type Classifier struct {
	db DB

	spam  map[string]int // used during training, persisted in Close
	total map[string]int // see above

	thresholdUnsure float64
	thresholdSpam   float64
}

func NewClassifier(db DB, thresholdUnsure, thresholdSpam float64) Classifier {
	return Classifier{
		db: db,

		spam:  make(map[string]int),
		total: make(map[string]int),

		thresholdUnsure: thresholdUnsure,
		thresholdSpam:   thresholdSpam,
	}
}

func (c Classifier) Persist(verbose bool) error {
	for word, diff := range c.total {
		err := c.db.Inc("total", word, diff, true)
		if err != nil {
			return fmt.Errorf("updating total for %q: %w", word, err)
		}
	}

	for word, diff := range c.spam {
		err := c.db.Inc("spam", word, diff, true)
		if err != nil {
			return fmt.Errorf("updating spam score for %q: %w", word, err)
		}
	}

	return nil

}

func (c Classifier) getWord(word string) (Word, error) {
	w := Word{
		Text: word,
	}

	w.Total = c.db.Get("total", word)
	w.Spam = c.db.Get("spam", word)

	return w, nil
}

// Train classifies the given word as spam or not spam, training c for future recognition.
func (c Classifier) Train(word string, spam bool, factor int) {
	c.total[word] += factor
	if spam {
		c.spam[word] += factor
	} else {
		c.spam[word] -= factor
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
