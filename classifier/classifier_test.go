package classifier

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"mailfilter/bloom"
	"math"
	"os"
	"sync"
	"testing"
)

const windowSize = 4

type Runnable interface {
	Run(context.Context)
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

type testDB struct {
	mu sync.Mutex

	m map[string]uint64
}

func (t *testDB) Add(w []byte, factor uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.m == nil {
		t.m = make(map[string]uint64)
	}

	t.m[string(w)] += factor
}

func (t *testDB) Remove(w []byte, factor uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.m == nil {
		t.m = make(map[string]uint64)
	}

	if t.m[string(w)] >= factor {
		t.m[string(w)] -= factor
	}
}

func (t *testDB) Score(w []byte) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.m[string(w)]
}

func TestClassifier_TrainSimple(t *testing.T) {
	words := []struct {
		word string
		spam bool
	}{
		{"foo", true},
		{"bar", false},
	}

	dbTotal := &testDB{}
	dbSpam := &testDB{}
	dbHam := &testDB{}

	c := New(dbTotal, dbHam, dbSpam, 0.3, 0.7, windowSize)

	for _, w := range words {
		err := c.trainWord([]byte(w.word), w.spam, 1)
		if err != nil {
			log.Fatalf("unexpected error: %s", err)
		}
	}

	if dbTotal.Score([]byte("foo")) != 1 || dbTotal.Score([]byte("bar")) != 1 {
		t.Errorf("unexpected total: %#v", dbTotal)
	}

	if dbSpam.Score([]byte("foo")) != 1 || dbSpam.Score([]byte("bar")) != 0 {
		t.Errorf("unexpected spam: %#v", dbSpam)
	}

	t.Logf("classifier: %#v", c)
}

func TestClassifier_Train(t *testing.T) {
	// First, test training
	words := []struct {
		word        string
		spam        bool
		expectScore float64
	}{
		{"bar", true, 1},
		{"bar", true, 1},
		{"fnord", false, 2.0 / 3},
		{"fnord", true, 2.0 / 3},
		{"fnord", true, 2.0 / 3},
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

	tmp := t.TempDir()

	dbTotal, err := bloom.NewDB(tmp, "total")
	if err != nil {
		t.Fatalf("can't create new bloom db: %s", err)
	}

	dbSpam, err := bloom.NewDB(tmp, "spam")
	if err != nil {
		t.Fatalf("can't create new bloom db: %s", err)
	}

	dbHam, err := bloom.NewDB(tmp, "ham")
	if err != nil {
		t.Fatalf("can't create new bloom db: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()

	wg.Add(3)

	run := func(db Runnable) {
		defer wg.Done()
		db.Run(ctx)
	}

	go run(dbHam)
	go run(dbSpam)
	go run(dbTotal)

	c := New(dbTotal, dbHam, dbSpam, 0.3, 0.7, windowSize)

	for _, w := range words {
		err := c.trainWord([]byte(w.word), w.spam, 1)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// Verify that the recorded spamminess is correct
	for i, w := range words {
		word, err := c.getWord([]byte(w.word))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		s := word.SpamLikelihood()
		if s != w.expectScore {
			t.Errorf("expected score %f, got %f for word %d: %v (db word: %v)", w.expectScore, s, i, w, word)
		}
	}
}

func TestClassifier_Text(t *testing.T) {
	tmp := t.TempDir()

	dbTotal, err := bloom.NewDB(tmp, "total")
	if err != nil {
		t.Fatalf("can't open bloom db: %s", err)
	}

	dbSpam, err := bloom.NewDB(tmp, "spam")
	if err != nil {
		t.Fatalf("can't open bloom db: %s", err)
	}

	dbHam, err := bloom.NewDB(tmp, "ham")
	if err != nil {
		t.Fatalf("can't open bloom db: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()

	wg.Add(3)

	run := func(db Runnable) {
		defer wg.Done()
		db.Run(ctx)
	}

	go run(dbHam)
	go run(dbSpam)
	go run(dbTotal)

	c := New(dbTotal, dbHam, dbSpam, 0.3, 0.7, windowSize)

	// Train the classifier
	textSpam := []string{
		"this is spam",
		"bitcoin is a good investment",
		"security update",
	}

	for _, txt := range textSpam {
		err := c.Train(bytes.NewBufferString(txt), true, 1)
		if err != nil {
			t.Fatalf("can't train text %q: %s", txt, err)
		}
	}

	textHam := []string{
		"how are you doing?",
		"foo is a fnord, so it's good. some bla as well.",
		"all of these worlds are yours, except europa. attempt no landing there.",
		"my friends are cool",
	}

	for _, txt := range textHam {
		err := c.Train(bytes.NewBufferString(txt), false, 1)
		if err != nil {
			t.Fatalf("can't train text %q: %s", txt, err)
		}
	}

	// Classify a few short texts
	texts := []struct {
		txt         string
		expectScore float64
		expectLabel string
	}{
		{"buy coins now", 0.9241, "spam"},
		{"foo fnord bla", 0, "ham"},
		{"asdf yes", 0.5, "unsure"},
		{"foo bar snafu", 0.0758, "ham"},
		{"i like my friend", 0, "ham"},
		{"this is spam", 1, "spam"},
	}

	epsilon := 1e-4

	for i, tc := range texts {
		buf := bytes.NewBufferString(tc.txt)

		s, err := c.Classify(buf, nil)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		t.Logf("text: %q, score: %s", tc.txt, s)

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
