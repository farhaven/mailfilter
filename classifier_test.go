package main

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/boltdb/bolt"
)

func TestWord_SpamLikelihood(t *testing.T) {
	testCases := []struct {
		name                 string
		word                 Word
		expectSpamLikelihood float64
	}{
		{
			name:                 "empty word (never seen)",
			expectSpamLikelihood: 0.5,
		},
		{
			name: "10% spamminess",
			word: Word{
				Total: 100,
				Spam:  10,
			},
			expectSpamLikelihood: 0.1,
		},
		{
			name: "90% spamminess",
			word: Word{
				Total: 100,
				Spam:  90,
			},
			expectSpamLikelihood: 0.9,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.word.SpamLikelihood()
			if s != tc.expectSpamLikelihood {
				t.Fatalf("expected spam likelihood %f, got %f for word %v", tc.expectSpamLikelihood, s, tc.word)
			}
		})
	}
}

func TestClassifier(t *testing.T) {
	// First, test training
	words := []struct {
		word        string
		spam        bool
		expectScore float64
	}{
		{"foo", false, 0},
		{"bar", true, 1},
		{"foo", false, 0},
		{"bar", true, 1},
		{"fnord", true, 1.0 / 3},
		{"fnord", false, 1.0 / 3},
		{"snafu", false, 0},
		{"fnord", false, 1.0 / 3},
		{"i", false, 0},
		{"like", false, 0},
		{"my", false, 0},
		{"friend", false, 0},
		{"this", true, 1},
		{"is", true, 1},
		{"spam", true, 1},
	}

	db, err := bolt.Open("words.db", 0600, nil)
	if err != nil {
		t.Fatalf("can't open db file: %s", err)
	}
	defer db.Close()

	c := NewClassifier(db)

	for _, w := range words {
		c.Train(w.word, w.spam)
	}

	// Verify that the recorded spamminess is correct
	for i, w := range words {
		word, err := c.getWord(w.word)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		s := word.SpamLikelihood()
		if s != w.expectScore {
			t.Errorf("expected score %f, got %f for word %d: %v", w.expectScore, s, i, w)
		}
	}

	// Classify a few short texts
	texts := []struct {
		txt         string
		expectScore float64
	}{
		{"foo fnord bla", 0},
		{"asdf yes", 0.5},
		{"foo bar snafu", 0},
		{"i like my friend", 0},
		{"this is spam", 1},
	}

	epsilon := 1e-4

	for i, tc := range texts {
		s, err := c.SpamLikelihood(tc.txt)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if s < 0 {
			t.Fatalf("score too low: %f", s)
		}

		if s > 1 {
			t.Fatalf("score too high: %f", s)
		}

		if math.Abs(s-tc.expectScore) > epsilon {
			t.Errorf("expected score %f, got %f for text %d: %q", tc.expectScore, s, i, tc.txt)
		}
	}
}

func TestSigmoid(t *testing.T) {
	testCases := []struct {
		x float64
	}{
		{0},
		{0.5},
		{1},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%f", tc.x), func(t *testing.T) {
			s := sigmoid(tc.x)

			if s <= 0 {
				t.Errorf("sigmoid too low for %f: %f", tc.x, s)
			}

			if s >= 1 {
				t.Errorf("sigmoid too high for %f: %f", tc.x, s)
			}
		})
	}
}

func TestMain(m *testing.M) {
	os.Remove("words.db")
	os.Exit(m.Run())
}
