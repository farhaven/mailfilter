package bloom

import (
	"strconv"
	"testing"
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
			t.Errorf("expected score >= 1 for %q, got %f", w, s)
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
			t.Errorf("expected score >= 1 for %q, got %f", w, s)
		}

	}

	for _, w := range words[split:] {
		s := f.Score([]byte(w))

		if s >= 1 && w != "foo" {
			t.Errorf("expected score < 1 for %q, got %f", w, s)
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
	words := []string{"a", "a", "b", "c"}

	var f1 F

	for _, w := range words {
		f1.Add([]byte(w))
	}

	for _, w := range words {
		s := f1.Score([]byte(w))
		t.Logf("score for %q: %f", w, s)
	}

	buf, err := f1.MarshalBinary()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := 1024
	if want != len(buf) {
		t.Errorf("unexpected length of encoded filter %d, want %d", len(buf), want)
	}

	var f2 F

	err = f2.UnmarshalBinary(buf)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	wantScores := map[string]float64{
		"a": 2,
		"b": 1,
		"c": 1,
	}

	for _, w := range words {
		s := f2.Score([]byte(w))
		if wantScores[w] != s {
			t.Errorf("expected score %f for %q, got %f", wantScores[w], w, s)
		}
	}
}

func TestBloom_RelativeScore(t *testing.T) {
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

		maybeBogus := sT < 1 // If total score is less than one, the data is likely bogus

		pSpam := (sS + 1) / (sT + 1)

		t.Logf("%-10s: %f / %f -> %f (%t/%t) bogus: %t", w, sS, sT, pSpam, isSpam[w], pSpam > 0.5, maybeBogus)
	}

	t.Fatal("eh")
}

func BenchmarkBloom_AddEncodeDecodeScore(b *testing.B) {
	b.ReportAllocs()

	strs := make([][]byte, 2*cacheSize)
	for i := 0; i < b.N && i < 2*cacheSize; i++ {
		strs[i] = []byte("foo1234567890qwertyuiopasdfghjklzxcvbnm" + strconv.Itoa(i))
	}

	b.ResetTimer()

	var f1 F

	for i := 0; i < b.N; i++ {
		f1.Add(strs[i%(2*cacheSize)])
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
			b.Fatalf("unexpected score for %s: %f != %f", s, s1, s2)
		}
	}
}
