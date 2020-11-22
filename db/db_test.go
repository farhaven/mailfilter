package db

import (
	"errors"
	"os"
	"strconv"
	"testing"
)

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
				err := db.Close()
				if err != nil {
					t.Error("unexpected error during close:", err)
				}
			}
		})
	}
}

func TestDB_SetGet(t *testing.T) {
	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := 234

	err = db.Inc("test", "foo", want)
	if err != nil {
		t.Error("unexpected error:", err)
	}

	got, err := db.Get("test", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if want != got {
		t.Errorf("unexpected value, want %d, got %d", want, got)
	}

	// Reopen db as readonly, assert that the value is still correct
	err = db.Close()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	db, err = Open("test.db", false)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	got, err = db.Get("test", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if want != got {
		t.Errorf("unexpected value, want %d, got %d", want, got)
	}

	// Attempt to write something to the (now readonly) db, assert that it fails
	err = db.Inc("test", "foo", 1234)
	if !errors.Is(err, ErrReadonly) {
		t.Errorf("expected readonly error, got %v", err)
	}

	err = db.Close()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	// Attempt to write something to the closed db, assert that it fails
	err = db.Inc("test", "foo", 1234)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected closed error, got %v", err)
	}
}

func TestDB_ManyGetSet(t *testing.T) {
	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	const howMany = 1e5

	for i := 0; i < howMany; i++ {
		err = db.Inc("test", strconv.Itoa(i), i)
		if err != nil {
			t.Error("unexpected error:", err)
		}
	}

	err = db.Close()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	db, err = Open("test.db", false)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	defer db.Close()

	for i := 0; i < howMany; i++ {
		got, err := db.Get("test", strconv.Itoa(i))
		if err != nil {
			t.Error("unexpected error:", err)
		}

		if i != got {
			t.Fatalf("unexpected value. want %d, got %d", i, got)
		}
	}
}

func TestDB_Clamp(t *testing.T) {
	incTest := func(db *DB, delta int, expect int) {
		now, err := db.Get("test", "counter")
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		if now < 0 {
			t.Errorf("got value outside of [0, inf): %d", now)
		}

		err = db.Inc("test", "counter", delta)
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		now, err = db.Get("test", "counter")
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		if now < 0 {
			t.Errorf("got value outside of [0, inf): %d", now)
		}

		if expect != now {
			t.Errorf("unexpected value: want %d, have %d", expect, now)
		}
	}

	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	incTest(db, 10, 10)
	incTest(db, -10, 0)
	incTest(db, -10, 0)
	incTest(db, 100, 100)
	incTest(db, -1000, 0)

	err = db.Close()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	db, err = Open("test.db", true)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	defer db.Close()

	incTest(db, 10, 10)
	incTest(db, -10, 0)
	incTest(db, -10, 0)
	incTest(db, 100, 100)
	incTest(db, -1000, 0)
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
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		err = db.Inc("test", "counter", 1)
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		now, err := db.Get("test", "counter")
		if err != nil {
			t.Fatal("unexpected error:", err)
		}

		want := i + 1
		if want != now {
			t.Errorf("unexpected counter: want %d, have %d", want, now)
		}

		err = db.Close()
		if err != nil {
			t.Fatal("unexpected error:", err)
		}
	}

	db, err := Open("test.db", false)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	defer func() {
		err := db.Close()
		if err != nil {
			t.Fatal("unexpected error:", err)
		}
	}()

	now, err := db.Get("test", "counter")
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	if wantIterations != now {
		t.Errorf("unexpected count: want %d, have %d", wantIterations, now)
	}
}

func BenchmarkLoadStore(b *testing.B) {
	const howMany = 1e6

	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	if err != nil {
		b.Fatal("unexpected error:", err)
	}

	for i := 0; i < howMany; i++ {
		db.Inc("test", strconv.Itoa(i), i+1)
	}

	db.Close()

	b.ResetTimer()

	for it := 0; it < b.N; it++ {
		db, err := Open("test.db", true)
		if err != nil {
			b.Fatal("unexpected error:", err)
		}

		for i := 0; i < howMany; i++ {
			db.Inc("test", strconv.Itoa(i), i+1)
		}

		db.Close()

		// Re-open DB readonly
		db, err = Open("test.db", false)
		if err != nil {
			b.Fatal("unexpected error:", err)
		}

		for i := 0; i < howMany; i += 2 {
			v, err := db.Get("test", strconv.Itoa(i))
			if err != nil {
				b.Fatalf("unexpected error for %d: %s", i, err)
			}

			if v == 0 {
				b.Fatalf("unexpected zero for %d", i)
			}
		}

		db.Close()
	}
}
