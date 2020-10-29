package main

import (
	"bytes"
	"io"
	"strconv"
	"unicode"
	"unicode/utf8"
)

type FilteredReaderState int

const (
	StateNormal FilteredReaderState = iota
	StatePunct
	StateNumber
	StateSeparator
	StateError
)

func (fs FilteredReaderState) String() string {
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

type FilteredReader struct {
	r io.Reader
	s FilteredReaderState
}

func NewFilteredReader(r io.Reader) *FilteredReader {
	return &FilteredReader{
		r: r,
		s: StateNormal,
	}
}

func (fr *FilteredReader) step(next FilteredReaderState) bool {
	rv := fr.s == next
	fr.s = next
	return rv
}

func (fr *FilteredReader) Read(data []byte) (int, error) {
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
