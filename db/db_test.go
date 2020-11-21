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

	err = db.Set("test", "foo", want)
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
	err = db.Set("test", "foo", 1234)
	if !errors.Is(err, ErrReadonly) {
		t.Errorf("expected readonly error, got %v", err)
	}

	err = db.Close()
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	// Attempt to write something to the closed db, assert that it fails
	err = db.Set("test", "foo", 1234)
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
		err = db.Set("test", strconv.Itoa(i), i)
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

func BenchmarkLoadStore(b *testing.B) {
	const howMany = 1e6

	os.RemoveAll("test.db")

	db, err := Open("test.db", true)
	if err != nil {
		b.Fatal("unexpected error:", err)
	}

	for i := 0; i < howMany; i++ {
		db.Set("test", strconv.Itoa(i), i+1)
	}

	db.Close()

	b.ResetTimer()

	for it := 0; it < b.N; it++ {
		db, err := Open("test.db", true)
		if err != nil {
			b.Fatal("unexpected error:", err)
		}

		for i := 0; i < howMany; i++ {
			db.Set("test", strconv.Itoa(i), i+1)
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
