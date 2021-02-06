package classifier

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/pkg/errors"
)

func ScanWords(data []byte, atEOF bool) (int, []byte, error) {
	advance, token, err := bufio.ScanWords(data, atEOF)

	if len(token) == 0 {
		return advance, token, err
	}

	tokLen := len(token)

	var runes []rune
	for len(token) > 0 {
		r, sz := utf8.DecodeRune(token)
		if sz == 0 {
			sz = 1 // Skip invalid runes
		}

		// Clean up runes, we're not that interested in specific punctuation and numbers
		switch {
		case unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsMark(r):
			r = '!'
		case unicode.IsControl(r) || r == utf8.RuneError:
			r = '*'
		case unicode.IsNumber(r):
			r = '#'
		}

		runes = append(runes, r)

		token = token[sz:]
	}

	// Sort runes
	sort.Slice(runes, func(i, j int) bool {
		return runes[i] < runes[j]
	})

	var (
		last       rune
		compressed []rune
	)
	for _, r := range runes {
		if last == r {
			continue
		}

		last = r

		compressed = append(compressed, last)
	}

	token = make([]byte, tokLen)
	idx := 0
	for _, r := range compressed {
		sz := utf8.EncodeRune(token[idx:], r)
		idx += sz
	}

	token = token[:idx]

	if len(token) > 128 {
		token = token[:128]
	}

	return advance, token, err
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

	if score < 0 || score > 1 {
		log.Printf("possibly corrupt database: score for {%q, %d, %d}: %f", w.Text, w.Total, w.Spam, score)
		score = 0.5
	}

	return score
}

func (w Word) String() string {
	return fmt.Sprintf("{%s %d %d -> %.3f}", w.Text, w.Total, w.Spam, w.SpamLikelihood())
}

type DB interface {
	Get(bucket string, key string) (int, error)
	Inc(bucket string, key string, delta int) error
}

type Classifier struct {
	db DB

	thresholdUnsure float64
	thresholdSpam   float64
}

func New(db DB, thresholdUnsure, thresholdSpam float64) *Classifier {
	return &Classifier{
		db: db,

		thresholdUnsure: thresholdUnsure,
		thresholdSpam:   thresholdSpam,
	}
}

func (c *Classifier) getWord(word string) (Word, error) {
	w := Word{
		Text: word,
	}

	var err error

	w.Total, err = c.db.Get("total", word)
	if err != nil {
		return w, err
	}

	w.Spam, err = c.db.Get("spam", word)
	if err != nil {
		return w, err
	}

	return w, nil
}

// Train classifies the given word as spam or not spam, training c for future recognition.
func (c *Classifier) Train(word string, spam bool, factor int) error {
	err := c.db.Inc("total", word, factor)
	if err != nil {
		return errors.Wrap(err, "incrementing total")
	}

	if spam {
		err = c.db.Inc("spam", word, factor)
	} else {
		err = c.db.Inc("spam", word, -factor)
	}

	if err != nil {
		return errors.Wrap(err, "incrementing spam count")
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
	return fmt.Sprintf("label=%q, score=%.6f", c.Label, c.Score)
}

// Classify classifies the given text and returns a label along with a "certainty" value for that label.
func (c *Classifier) Classify(text io.Reader) (ClassificationResult, error) {
	scanner := bufio.NewScanner(text)
	scanner.Split(ScanWords)

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
