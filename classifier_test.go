package main

import (
	"bufio"
	"bytes"
	"fmt"
	"mailfilter/db"
	"math"
	"os"
	"regexp"
	"strconv"
	"testing"
)

func TestScan(t *testing.T) {
	testCases := []struct {
		txt         string
		expectWords []string
	}{
		{
			txt:         "foo, bar asd2123aaa yellow :)  ",
			expectWords: []string{"foo!", "bar", "asd#aaa", "yellow", "!"},
		},
		{
			txt:         "green GREEN grEEn gr33n",
			expectWords: []string{"green", "green", "green", "gr#n"},
		},
		{
			txt:         "foo 123 bar :) asdf",
			expectWords: []string{"foo", "#", "bar", "!", "asdf"},
		},
		{
			txt:         "averylongwordindeedprobablylongerthansixteencharacters",
			expectWords: []string{"averylongwordind", "eedprobablylonge", "rthansixteenchar", "acters"},
		},
	}

	expr := regexp.MustCompile(`^[\p{Ll}!#]+$`)

	for idx, tc := range testCases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			buf := bytes.NewBufferString(tc.txt)

			scanner := bufio.NewScanner(NewFilteredReader(buf))
			scanner.Split(ScanNGram)

			wordIdx := 0
			for ; scanner.Scan(); wordIdx++ {
				word := scanner.Text()
				t.Logf("got word: %s", word)

				if !expr.Match([]byte(word)) {
					t.Errorf("%q does not match %s", word, expr)
				}

				if len(tc.expectWords) < (wordIdx + 1) {
					t.Errorf("got unexpected word %q", word)
				}

				if len(tc.expectWords) > wordIdx && tc.expectWords[wordIdx] != word {
					t.Errorf("expected %q at %d, got %q", tc.expectWords[wordIdx], idx, word)
				}
			}

			if len(tc.expectWords) != wordIdx {
				t.Errorf("expected %d words, got %d", len(tc.expectWords), wordIdx)
			}
		})
	}
}

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

func TestClassifier_Train(t *testing.T) {
	words := []struct {
		word string
		spam bool
	}{
		{"foo", true},
		{"bar", false},
	}

	db, err := db.Open("words.db", true)
	if err != nil {
		t.Fatalf("can't open db file: %s", err)
	}
	defer db.Close()

	c := NewClassifier(db, 0.3, 0.7)

	for _, w := range words {
		c.Train(w.word, w.spam, 1)
	}

	if c.total["foo"] != 1 || c.total["bar"] != 1 {
		t.Errorf("unexpected total: %#v", c.total)
	}

	if c.spam["foo"] != 1 || c.spam["bar"] != -1 {
		t.Errorf("unexpected spam: %#v", c.spam)
	}

	t.Logf("classifier: %#v", c)
}

func TestClassifier(t *testing.T) {
	// First, test training
	words := []struct {
		word        string
		spam        bool
		expectScore float64
	}{
		{"bar", true, 1},
		{"bar", true, 1},
		{"fnord", false, 1.0 / 3},
		{"fnord", true, 1.0 / 3},
		{"fnord", true, 1.0 / 3},
		{"foo", false, 0},
		{"foo", false, 0},
		{"foo", false, 0},
		{"foo", false, 0},
		{"foo", false, 0},
		{"foo", false, 0},
		{"foo", false, 0},
		{"friend", false, 0},
		{"i", false, 0},
		{"is", true, 1},
		{"like", false, 0},
		{"my", false, 0},
		{"snafu", false, 0},
		{"spam", true, 1},
		{"this", true, 1},
	}

	db, err := db.Open("words.db", true)
	if err != nil {
		t.Fatalf("can't open db file: %s", err)
	}
	defer db.Close()

	c := NewClassifier(db, 0.3, 0.7)

	for _, w := range words {
		c.Train(w.word, w.spam, 1)
	}

	err = c.Persist()
	if err != nil {
		t.Fatalf("can't persist trained data: %s", err)
	}

	// Verify that the recorded spamminess is correct
	for i, w := range words {
		word, err := c.getWord(w.word)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		s := word.SpamLikelihood()
		if s != w.expectScore {
			t.Errorf("expected score %f, got %f for word %d: %v (db word: %v)", w.expectScore, s, i, w, word)
		}
	}

	// Classify a few short texts
	texts := []struct {
		txt         string
		expectScore float64
		expectLabel string
	}{
		{"foo fnord bla", 0, "ham"},
		{"asdf yes", 0.5, "unsure"},
		{"foo bar snafu", 0, "ham"},
		{"i like my friend", 0, "ham"},
		{"this is spam", 1, "spam"},
	}

	epsilon := 1e-4

	for i, tc := range texts {
		buf := bytes.NewBufferString(tc.txt)

		s, err := c.Classify(buf)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if s.Score < 0 {
			t.Fatalf("score too low: %s", s)
		}

		if s.Score > 1 {
			t.Fatalf("score too high: %s", s)
		}

		if math.Abs(s.Score-tc.expectScore) > epsilon {
			t.Errorf("expected score %f, got %f for text %d: %q", tc.expectScore, s.Score, i, tc.txt)
		}

		if tc.expectLabel != s.Label {
			t.Errorf("expected label %q, got %q for text %d: %q", tc.expectLabel, s.Label, i, tc.txt)
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
	err := os.RemoveAll("words.db")
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}
