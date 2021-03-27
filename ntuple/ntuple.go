package ntuple

import (
	"io"

	"github.com/pkg/errors"
)

const bufSz = 4096 * 1024

// A Reader produces subsequent substrings of a predefined length from an io.Reader:
//
//  r := New(bytes.NewBufferString("123456"))
//  buf := make([]byte, 3)
//
//  // Each call to in.Next(buf) will fill buf with the following contents
//  "123"
//  "234"
//  "456"
type Reader struct {
	buf []byte
	in  io.Reader
}

// New creates a Reader with the given input.
func New(in io.Reader) Reader {
	return Reader{
		in: in,
	}
}

// Next fills d with the next subslice of data from r's input reader. Next will return
// io.EOF when the input reader has been exhausted, and it will return all other errors
// produced by the underlying reader as they come.
// Next will skip chunks with ASCII control bytes (less than 0x20) or invalid UTF8 bytes
// in them. Invalid UTF8 bytes are 0xC1, 0xC2 and 0xF5 to 0xFD
func (r *Reader) Next(d []byte) error {
	for {
		if len(r.buf) < len(d) {
			r.buf = make([]byte, bufSz)
			n, err := r.in.Read(r.buf)
			if err != nil && !errors.Is(err, io.EOF) {
				return errors.Wrapf(err, "reading from underlying after %d bytes", n)
			}

			r.buf = r.buf[:n]
		}

		if len(r.buf) < len(d) {
			return io.EOF
		}

		foundControl := false
		for idx := 0; idx < len(d); idx++ {
			b := r.buf[idx]

			if b < 0x20 {
				foundControl = true
				break
			}

			if b == 0xC1 || b == 0xC2 {
				foundControl = true
				break
			}

			if b >= 0xF5 && b <= 0xFD {
				foundControl = true
				break
			}

			d[idx] = b
		}

		r.buf = r.buf[1:]

		if !foundControl {
			break
		}
	}

	return nil
}
