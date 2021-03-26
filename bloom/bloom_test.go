package bloom

import (
	"errors"
	"math"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	"testing"
	"time"

	"github.com/boltdb/bolt"
)

func TestBloom(t *testing.T) {
	f := F{}

	words := []string{"foo", "bar", "fnord", "foo", "yes", "something"}

	for _, w := range words {
		f.Add([]byte(w))
	}

	for _, w := range words {
		s := f.Score([]byte(w))

		if s < 1 {
			t.Errorf("expected score >= 1 for %q, got %v", w, s)
		}
	}

	words = append(words, []string{"aaa", "bbb"}...)

	const split = 2

	for _, w := range words[split:] {
		f.Remove([]byte(w))
	}

	for _, w := range words[:split] {
		s := f.Score([]byte(w))

		if s < 1 {
			t.Errorf("expected score >= 1 for %q, got %v", w, s)
		}

	}

	for _, w := range words[split:] {
		s := f.Score([]byte(w))

		if s >= 1 && w != "foo" {
			t.Errorf("expected score < 1 for %q, got %v", w, s)
		}
	}
}

func TestBloom_HowManyFnords(t *testing.T) {
	f := F{}

	var rounds int
	for rounds = 1; ; rounds++ {
		f.Add([]byte("fnord"))
		if f.Score([]byte("fnord")) >= 1 {
			t.Logf("needed %d fnords", rounds)
			break
		}
	}

	if rounds > 10 {
		t.Errorf("expected less than 10 rounds, but needed %d", rounds)
	}
}

func TestBloom_EncodeDecode(t *testing.T) {
	t.Skip("eh")

	words := []string{"a", "a", "b", "c"}

	var f1 F

	for _, w := range words {
		f1.Add([]byte(w))
	}

	for _, w := range words {
		s := f1.Score([]byte(w))
		t.Logf("score for %q: %v", w, s)
	}

	buf, err := f1.MarshalBinary()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := filterSize * 8
	if want != len(buf) {
		t.Errorf("unexpected length of encoded filter %d, want %d", len(buf), want)
	}

	var f2 F

	err = f2.UnmarshalBinary(buf)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	wantScores := map[string]uint64{
		"a": 2,
		"b": 1,
		"c": 1,
	}

	for _, w := range words {
		s := f2.Score([]byte(w))

		if wantScores[w] != s {
			t.Errorf("expected score %v for %q, got %v", wantScores[w], w, s)
		}
	}
}

func TestBloom_RelativeScore(t *testing.T) {
	t.Skip("not done yet")

	spam := []string{"foo", "bar", "fnord"}
	words := append([]string{"foo", "this", "is", "a", "test", "foo", "bar", "fnord", "a", "b", "c"}, spam...)

	isSpam := make(map[string]bool)
	for _, w := range spam {
		isSpam[w] = true
	}

	var (
		fSpam  F
		fTotal F
	)

	for _, w := range words {
		fTotal.Add([]byte(w))
	}

	for _, w := range spam {
		fSpam.Add([]byte(w))
	}

	words = append(words, "yellow", "pinata", "potato", "bitcoin", "schmitcoin", "flipcoin", "precisecoin")

	for _, w := range words {
		sS := fSpam.Score([]byte(w))
		sT := fTotal.Score([]byte(w))

		pSpam := float64(sS+1) / float64(sT+1)

		t.Logf("%-10s: %v / %v -> %f (%t/%t)", w, sS, sT, pSpam, isSpam[w], pSpam > 0.5)
	}

	t.Fatal("eh")
}

func BenchmarkBloom_AddEncodeDecodeScore(b *testing.B) {
	const numEntries = 4096

	b.ReportAllocs()

	strs := make([][]byte, 2*numEntries)
	for i := 0; i < b.N && i < 2*numEntries; i++ {
		strs[i] = []byte("foo1234567890qwertyuiopasdfghjklzxcvbnm" + strconv.Itoa(i))
	}

	b.ResetTimer()

	var f1 F

	for i := 0; i < b.N; i++ {
		f1.Add(strs[i%(2*numEntries)])
	}

	buf, err := f1.MarshalBinary()
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	var f2 F

	err = f2.UnmarshalBinary(buf)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	for _, s := range strs {
		s1 := f1.Score(s)
		s2 := f2.Score(s)

		if s1 != s2 {
			b.Fatalf("unexpected score for %s: %v != %v", s, s1, s2)
		}
	}
}

func BenchmarkF_AddTest(b *testing.B) {
	txt := []byte("abcdefghijklmnopqrstuvwxyz")

	b.ReportAllocs()
	b.ResetTimer()

	f := F{}
	for i := 0; i < b.N; i++ {
		f.Add(txt)
	}
}

func BenchmarkF_AddTestData(b *testing.B) {
	// Load test data from bolt, then insert each element into the filter, repeatedly
	// As a benchmark metric, report the load of the filter:
	// - number of non-zero fields/total number of fields
	// - average of field values, with min and max
	go func() {
		err := http.ListenAndServe(":6060", nil)
		if err != nil {
			panic("can't run profiling server: " + err.Error())
		}
	}()

	started := time.Now()

	type elem struct {
		w []byte
		c int
	}

	total := make([]elem, 0)

	bDB, err := bolt.Open("testdata", 0600, nil)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	err = bDB.View(func(t *bolt.Tx) error {
		bTotal := t.Bucket([]byte("total"))
		if bTotal == nil {
			return errors.New("total bucket empty")
		}

		err = bTotal.ForEach(func(k, v []byte) error {
			key := string(k)
			val, err := strconv.Atoi(string(v))
			if err != nil {
				return err
			}

			total = append(total, elem{
				w: []byte(key),
				c: val,
			})

			return nil
		})

		if err != nil {
			return err
		}

		return nil
	})

	bDB.Close()

	b.Logf("took %s to load test data, have %d keys", time.Since(started), len(total))

	b.ResetTimer()
	b.ReportAllocs()

	f := F{}

	for i := 0; i < b.N; i++ {
		if i > 1 {
			b.Fatal("benchmark is broken after one iteration")
		}

		// Insert all test data elements into the bloom filter
		for _, v := range total {
			for ; v.c > 0; v.c-- {
				f.Add(v.w)
			}
		}
	}

	var (
		mse    float64
		errors int
	)

	for _, item := range total {
		score := float64(f.Score(item.w))

		mse += math.Pow(float64(item.c)-score, 2)

		l1 := math.Log(float64(item.c)) / math.Log(10)
		l2 := math.Log(score) / math.Log(10)

		if math.Abs(l1-l2) > 2 {
			b.Logf("got unexpected score for %q: want %v (%v), have %v (%v)", item.w, item.c, l1, score, l2)
			errors++
		}
	}

	mse /= float64(len(total))

	b.ReportMetric(mse, "mse")
	b.ReportMetric(float64(errors), "errors")
}
