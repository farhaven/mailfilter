package filtered

import (
	"bytes"
	"io"
	"strconv"
	"unicode"
	"unicode/utf8"
)

type ReaderState int

const (
	StateNormal ReaderState = iota
	StatePunct
	StateNumber
	StateSeparator
	StateError
)

func (fs ReaderState) String() string {
	switch fs {
	case StateNormal:
		return "Normal"
	case StatePunct:
		return "Punctuation"
	case StateNumber:
		return "Number"
	case StateSeparator:
		return "Separator"
	case StateError:
		return "Error"
	default:
		panic("unexpected state: " + strconv.Itoa(int(fs)))
	}
}

type Reader struct {
	r io.Reader
	s ReaderState
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r: r,
		s: StateNormal,
	}
}

func (fr *Reader) step(next ReaderState) bool {
	rv := fr.s == next
	fr.s = next
	return rv
}

func (fr *Reader) Read(data []byte) (int, error) {
	n, err := fr.r.Read(data)
	if err != nil {
		return 0, err
	}

	lowercase := bytes.ToLower(data[:n])

	writeIdx := 0

	for len(lowercase) > 0 {
		r, sz := utf8.DecodeRune(lowercase)
		if r == utf8.RuneError {
			sz = 1 // Force skip, even if the rune is short
		}
		lowercase = lowercase[sz:]

		switch {
		case r == utf8.RuneError || unicode.IsControl(r):
			if fr.step(StateError) {
				// Already inside a non-utf8 or control sequence
				continue
			}

			data[writeIdx] = '*'
		case unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsMark(r):
			if fr.step(StatePunct) {
				// Already inside a sequence of punctuation
				continue
			}

			data[writeIdx] = '!'
		case unicode.IsNumber(r):
			if fr.step(StateNumber) {
				continue
			}

			data[writeIdx] = '#'
		case unicode.IsSpace(r):
			if fr.step(StateSeparator) {
				continue
			}

			data[writeIdx] = ' '
		default:
			// Encode rune into output slice
			// NB: Since we use a copy of data as the input, there should (tm) always be enough space in the remainder of data to encode the rune.
			writeIdx += utf8.EncodeRune(data[writeIdx:], r)
			fr.step(StateNormal)
			continue
		}

		writeIdx++
	}

	return writeIdx, nil
}
