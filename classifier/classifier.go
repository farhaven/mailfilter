package classifier

import (
	"fmt"
	"io"
	"log"
	"math"

	"github.com/pkg/errors"

	"mailfilter/ntuple"
)

const windowSize = 6

type Word struct {
	Text []byte

	// Number of times this word has been seen in all messages and in spam messages
	Total uint64
	Spam  uint64
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
		log.Printf("possibly corrupt database: score for {%q, %v, %v}: %f", w.Text, w.Total, w.Spam, score)
		score = 0.5
	}

	return score
}

func (w Word) String() string {
	return fmt.Sprintf("{%q %v %v → %.3f}", w.Text, w.Total, w.Spam, w.SpamLikelihood())
}

type DB interface {
	Add([]byte, uint64)
	Remove([]byte, uint64)
	Score([]byte) uint64 // (approximate) count of times that the sequences has been added to the db
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

	return w, nil
}

func (c *Classifier) Train(in io.Reader, spam bool, learnFactor uint64) error {
	buf := make([]byte, windowSize)
	reader := ntuple.New(in)

	for {
		err := reader.Next(buf)
		if err != nil && errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		err = c.trainWord(buf, spam, learnFactor)
		if err != nil {
			return err
		}
	}

	return nil
}

// trainWord classifies the given word as spam or not spam, training c for future recognition.
func (c *Classifier) trainWord(word []byte, spam bool, factor uint64) error {
	c.dbTotal.Add(word, factor)
	if spam {
		c.dbSpam.Add(word, factor)
	}

	return nil
}

func sigmoid(x float64) float64 {
	if x < 0 || x > 1 {
		panic(fmt.Sprintf("x out of [0, 1]: %f", x))
	}

	midpoint := 0.5
	max := 1.0
	k := 5.0

	return max / (1.0 + math.Exp(-k*(x-midpoint)))
}

type Result struct {
	Label string
	Score float64
	Eta   float64
	Min   float64
	Max   float64
}

func (c Result) String() string {
	return fmt.Sprintf("label=%q, score=%.6f, η:%f [%f, %f]", c.Label, c.Score, c.Eta, c.Min, c.Max)
}

// Classify classifies the given text and returns a label along with a "certainty" value for that label.
func (c *Classifier) Classify(text io.Reader, verbose io.Writer) (Result, error) {
	reader := ntuple.New(text)

	buf := make([]byte, windowSize)

	const alpha = 0.975

	var eta float64

	min := math.Inf(1)
	max := math.Inf(-1)

	for {
		err := reader.Next(buf)
		if err != nil && errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Println("reading input:", err)
			break
		}

		word, err := c.getWord(buf)
		if err != nil {
			return Result{}, errors.Wrap(err, "getting word counts")
		}

		p := word.SpamLikelihood()

		// Pass scores through a tuned sigmoid so that they stay strictly above 0 and
		// strictly below 1. This makes calculating with the inverse a bit easier, at
		// the expense of never returning an absolute verdict, and slightly biasing
		// towards detecting stuff as ham, since sigmoid(0.5) < 0.5.
		s := sigmoid(p)

		l1 := math.Log(1 - s)
		l2 := math.Log(s)

		if math.IsNaN(l1) || math.IsInf(l1, 0) {
			panic(fmt.Sprintf("l1: %f %f %f", l1, 1-p, 1-s))
		}

		if math.IsNaN(l2) || math.IsInf(l2, 0) {
			panic(fmt.Sprintf("l2: %f %f %f", l2, p, s))
		}

		eta *= alpha
		eta += l1 - l2

		if min > eta {
			min = eta
		}

		if max < eta {
			max = eta
		}

		if math.IsNaN(eta) || math.IsInf(eta, 0) {
			panic(fmt.Sprintf("eta: %f", eta))
		}

		if verbose != nil {
			fmt.Fprintf(verbose, "%s: %f, l:[%f, %f], η:%f, current score:%f\n", word, s, l1, l2, eta, 1.0/(1.0+math.Exp(eta)))
		}
	}

	if verbose != nil {
		fmt.Fprintln(verbose, "final η:", eta, "min η:", min, "max η:", max)
	}

	score := 1.0 / (1.0 + math.Exp(eta))
	if math.IsNaN(score) || math.IsInf(score, 0) {
		panic(fmt.Sprintf("score: %f", score))
	}

	result := Result{
		Score: score,
		Label: "ham",
		Eta:   eta,
		Max:   max,
		Min:   min,
	}

	if result.Score > c.thresholdUnsure {
		result.Label = "unsure"
	}

	if result.Score > c.thresholdSpam {
		result.Label = "spam"
	}

	return result, nil
}
