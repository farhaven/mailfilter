package db

import (
	"os"
	"strconv"
	"testing"
)

type Fataler interface {
	Fatal(...interface{})
}

func expectNoError(t Fataler, err error) {
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
}

func TestDB_Open(t *testing.T) {
	os.RemoveAll("test.db")

	testCases := []struct {
		name      string
		writeable bool
	}{
		{name: "readonly, not existing", writeable: false},
		{name: "rw, not existing", writeable: true},
		{name: "readonly, existing", writeable: false},
		{name: "rw, existing", writeable: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open("test.db", tc.writeable)

			if err != nil {
				t.Error("unexpected error during open:", err)
			} else {
				expectNoError(t, db.Close())
			}
		})
	}
}

func TestDB_SetGet(t *testing.T) {
	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	expectNoError(t, err)

	want := 234

	expectNoError(t, db.Inc("test", "foo", want))

	got, err := db.Get("test", "foo")
	expectNoError(t, err)

	if want != got {
		t.Errorf("unexpected value, want %d, got %d", want, got)
	}

	// Reopen db as readonly, assert that the value is still correct
	expectNoError(t, db.Close())

	db, err = Open("test.db", false)
	expectNoError(t, err)

	got, err = db.Get("test", "foo")
	expectNoError(t, err)

	if want != got {
		t.Errorf("unexpected value, want %d, got %d", want, got)
	}

	// Attempt to write something to the (now readonly) db, assert that it fails
	err = db.Inc("test", "foo", 1234)
	if err != nil {
		t.Errorf("got unexpected error: %v", err)
	}

	expectNoError(t, db.Close())
}

func TestDB_ManyGetSet(t *testing.T) {
	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	expectNoError(t, err)

	const howMany = 1e5

	for i := 0; i < howMany; i++ {
		expectNoError(t, db.Inc("test", strconv.Itoa(i), i))
	}

	expectNoError(t, db.Close())

	db, err = Open("test.db", false)
	expectNoError(t, err)
	defer db.Close()

	for i := 0; i < howMany; i++ {
		got, err := db.Get("test", strconv.Itoa(i))
		expectNoError(t, err)

		if i != got {
			t.Fatalf("unexpected value. want %d, got %d", i, got)
		}
	}
}

func TestDB_Clamp(t *testing.T) {
	incTest := func(db *DB, delta int) {
		before, err := db.Get("test", "counter")
		expectNoError(t, err)

		if before < 0 {
			t.Errorf("got value outside of [0, inf): %d", before)
		}

		expectNoError(t, db.Inc("test", "counter", delta))

		after, err := db.Get("test", "counter")
		expectNoError(t, err)

		if after < 0 {
			t.Errorf("got value outside of [0, inf): %d", before)
		}
	}

	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	expectNoError(t, err)

	incTest(db, 10)
	incTest(db, -10)
	incTest(db, -10)
	incTest(db, 101)
	incTest(db, -1000)

	expectNoError(t, db.Close())

	db, err = Open("test.db", true)
	expectNoError(t, err)
	defer db.Close()

	incTest(db, 10)
	incTest(db, -10)
	incTest(db, -10)
	incTest(db, 100)
	incTest(db, -1000)
}

func TestDB_SequentialModify(t *testing.T) {
	// This test is for the following scenario:
	// A DB is created, a counter is initialized, the DB is closed
	// The db is reopened, the counter increased, the DB is closed
	// The db is opened again, the counter increased, the DB is closed
	// The db is opened readonly, the counter is read
	os.RemoveAll("test.db")

	const wantIterations = 10

	for i := 0; i < wantIterations; i++ {
		db, err := Open("test.db", true)
		expectNoError(t, err)

		expectNoError(t, db.Inc("test", "counter", 1))

		now, err := db.Get("test", "counter")
		expectNoError(t, err)

		want := i + 1
		if want != now {
			t.Errorf("unexpected counter: want %d, have %d", want, now)
		}

		expectNoError(t, db.Close())
	}

	db, err := Open("test.db", false)
	expectNoError(t, err)
	defer func() {
		expectNoError(t, db.Close())
	}()

	now, err := db.Get("test", "counter")
	expectNoError(t, err)

	if wantIterations != now {
		t.Errorf("unexpected count: want %d, have %d", wantIterations, now)
	}
}

func BenchmarkLoadStore(b *testing.B) {
	const howMany = 1e5

	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	expectNoError(b, err)

	for i := 0; i < howMany; i++ {
		expectNoError(b, db.Inc("test", strconv.Itoa(i), i+1))
	}

	expectNoError(b, db.Close())

	b.ResetTimer()

	for it := 0; it < b.N; it++ {
		db, err := Open("test.db", true)
		expectNoError(b, err)

		for i := 0; i < howMany; i++ {
			expectNoError(b, db.Inc("test", strconv.Itoa(i), i+1))
		}

		expectNoError(b, db.Close())

		// Re-open DB readonly
		db, err = Open("test.db", false)
		expectNoError(b, err)

		for i := 0; i < howMany; i += 2 {
			v, err := db.Get("test", strconv.Itoa(i))
			expectNoError(b, err)

			if v == 0 {
				b.Fatalf("unexpected zero for %d", i)
			}
		}

		expectNoError(b, db.Close())
	}
}

func BenchmarkUpdate(b *testing.B) {
	const howMany = 1e5

	os.RemoveAll("test.db")

	b.ResetTimer()

	for it := 0; it < b.N; it++ {
		db, err := Open("test.db", true)
		expectNoError(b, err)

		for c := 0; c < howMany; c++ {
			expectNoError(b, db.Inc("test", "bench-"+strconv.Itoa(c), 1))
		}

		expectNoError(b, db.Close())
	}
}
