package classifier

import (
	"fmt"
	"io"
	"log"
	"math"

	"github.com/pkg/errors"

	"mailfilter/ntuple"
)

type Word struct {
	Text  []byte
	Total float64
	Spam  float64
}

func (w Word) SpamLikelihood() float64 {
	if w.Total < 1 {
		// haven't seen this word yet, can't say anything about it.
		return 0.5
	}

	score := float64(w.Spam+1) / float64(w.Total+1)

	if math.IsInf(score, 0) {
		panic(fmt.Sprintf("infinite score for %v", w))
	}

	if math.IsNaN(score) {
		panic(fmt.Sprintf("nan score for %v", w))
	}

	if score < 0 || score > 1 {
		log.Printf("possibly corrupt database: score for {%q, %v, %v}: %f", w.Text, w.Total, w.Spam, score)
		score = 0.5
	}

	return score
}

func (w Word) String() string {
	return fmt.Sprintf("{%s %v %v -> %.3f}", w.Text, w.Total, w.Spam, w.SpamLikelihood())
}

type DB interface {
	Add([]byte)
	Remove([]byte)
	Score([]byte) float64
}

type Classifier struct {
	dbTotal DB
	dbSpam  DB

	thresholdUnsure float64
	thresholdSpam   float64
}

func New(dbTotal, dbSpam DB, thresholdUnsure, thresholdSpam float64) *Classifier {
	return &Classifier{
		dbTotal: dbTotal,
		dbSpam:  dbSpam,

		thresholdUnsure: thresholdUnsure,
		thresholdSpam:   thresholdSpam,
	}
}

func (c *Classifier) getWord(word []byte) (Word, error) {
	w := Word{
		Text:  word,
		Total: c.dbTotal.Score(word),
		Spam:  c.dbSpam.Score(word),
	}

	if w.Total > w.Spam {
		w.Total = w.Spam
	}

	return w, nil
}

// Train classifies the given word as spam or not spam, training c for future recognition.
func (c *Classifier) Train(word []byte, spam bool, factor int) error {
	c.dbTotal.Add(word)
	if spam {
		c.dbSpam.Add(word)
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
	reader := ntuple.New(text)

	buf := make([]byte, 4)

	var scores []float64
	for {
		err := reader.Next(buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ClassificationResult{}, errors.Wrap(err, "reading input")
		}

		word, err := c.getWord(buf)
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
