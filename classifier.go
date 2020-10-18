package main

import (
	"fmt"
	"math"
	"strings"
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
	words map[string]Word
}

func NewClassifier() Classifier {
	return Classifier{
		words: make(map[string]Word),
	}
}

// Train classifies the given word as spam or not spam, training c for future recognition.
func (c Classifier) Train(word string, spam bool) {
	w, ok := c.words[word]
	if !ok {
		w = Word{}
	}

	w.Total++
	if spam {
		w.Spam++
	}

	c.words[word] = w
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

// SpamLikelihood returns the probability that the given text is spam.
func (c Classifier) SpamLikelihood(text string) float64 {
	// TODO: This doesn't deal with float underflows. That's probably not good.
	//       Calculating this stuff in the log-domain doesn't seem to work though,
	//       or I'm too stupid. That seems more likely.

	words := strings.Fields(text)

	var scores []float64
	for _, w := range words {
		p := c.words[w].SpamLikelihood()

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

	return a / (a + b)
}
