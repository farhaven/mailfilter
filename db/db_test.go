package db

import (
	"errors"
	"os"
	"testing"
)

func TestDB_Open(t *testing.T) {
	os.Remove("test.db")

	testCases := []struct {
		name        string
		writeable   bool
		expectError bool
	}{
		{name: "readonly, not existing", writeable: false, expectError: true},
		{name: "rw, not existing", writeable: true, expectError: false},
		{name: "readonly, existing", writeable: false, expectError: false},
		{name: "rw, existing", writeable: true, expectError: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open("test.db", tc.writeable)

			if tc.expectError && err == nil {
				t.Error("expected an error, got nil")
			} else if !tc.expectError {
				if err != nil {
					t.Error("unexpected error during open:", err)
				} else {
					err := db.Close()
					if err != nil {
						t.Error("unexpected error during close:", err)
					}
				}
			}
		})
	}
}

func TestDB_SetGet(t *testing.T) {
	os.Remove("test.db")

	db, err := Open("test.db", true)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	want := 234

	err = db.Set("test", "foo", want)
	if err != nil {
		t.Error("unexpected error:", err)
	}

	got := db.Get("test", "foo")

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

	got = db.Get("test", "foo")

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
