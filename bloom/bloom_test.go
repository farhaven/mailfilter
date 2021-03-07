package bloom

import (
	"strconv"
	"testing"
)

func TestBloom(t *testing.T) {
	f := Bloom{}

	words := []string{"foo", "bar", "fnord", "foo", "yes", "something"}

	for _, w := range words {
		f.add(w)
	}

	for _, w := range words {
		s := f.score(w)

		if s < 1 {
			t.Errorf("expected score >= 1 for %q, got %f", w, s)
		}
	}

	words = append(words, []string{"aaa", "bbb"}...)

	const split = 2

	for _, w := range words[split:] {
		f.remove(w)
	}

	for _, w := range words[:split] {
		s := f.score(w)

		if s < 1 {
			t.Errorf("expected score >= 1 for %q, got %f", w, s)
		}

	}

	for _, w := range words[split:] {
		s := f.score(w)

		if s >= 1 && w != "foo" {
			t.Errorf("expected score < 1 for %q, got %f", w, s)
		}
	}
}

func TestBloom_HowManyFnords(t *testing.T) {
	f := Bloom{}

	var rounds int
	for rounds = 1; ; rounds++ {
		f.add("fnord")
		if f.score("fnord") >= 1 {
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

	var f1 Bloom

	for _, w := range words {
		f1.add(w)
	}

	for _, w := range words {
		s := f1.score(w)
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

	var f2 Bloom

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
		s := f2.score(w)
		if wantScores[w] != s {
			t.Errorf("expected score %f for %q, got %f", wantScores[w], w, s)
		}
	}
}

func BenchmarkBloom_AddEncodeDecodeScore(b *testing.B) {
	b.ReportAllocs()

	strs := make([]string, 2*cacheSize)
	for i := 0; i < b.N && i < 2*cacheSize; i++ {
		strs[i] = "foo1234567890qwertyuiopasdfghjklzxcvbnm" + strconv.Itoa(i)
	}

	b.ResetTimer()

	var f1 Bloom

	for i := 0; i < b.N; i++ {
		f1.add(strs[i%(2*cacheSize)])
	}

	buf, err := f1.MarshalBinary()
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	var f2 Bloom

	err = f2.UnmarshalBinary(buf)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	for _, s := range strs {
		s1 := f1.score(s)
		s2 := f2.score(s)
		if s1 != s2 {
			b.Fatalf("unexpected score for %s: %f != %f", s, s1, s2)
		}
	}
}
