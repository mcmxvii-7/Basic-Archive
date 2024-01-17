package bar

import (
	"bufio"
	"compress/flate"
	"encoding/binary"
	"errors"
	"hash"
	"hash/adler32"
	"io"
	"slices"
)

var (
	ErrUnknownFormat      = errors.New("Unknown file format.")
	ErrUnsupportedVersion = errors.New("Unsupported BAR version.")
	ErrInvalidChecksum    = errors.New("Invalid checksum.")
)

type Reader struct {
	Entries []Entry
	r       io.ReadSeeker
}

func NewReader(r io.ReadSeeker) (*Reader, error) {
	header := make([]byte, headerSize)
	_, err := r.Read(header)
	if err == io.EOF {
		return nil, io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, err
	}

	magic := header[0:3]
	version := header[3]

	if !slices.Equal(magic, magicNumber) {
		return nil, ErrUnknownFormat
	}

	if version != Version {
		return nil, ErrUnsupportedVersion
	}

	_, err = r.Seek(-footerSize, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	footer := make([]byte, footerSize)
	_, err = r.Read(footer)
	if err == io.EOF {
		return nil, io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, err
	}

	rb := rBuf(footer)
	table := rb.Uint64()
	adler := rb.Uint32()
	count := rb.Uint32()

	_, err = r.Seek(int64(table), io.SeekStart)
	if err != nil {
		return nil, err
	}

	ar := newAdlerReader(r)
	fr := flate.NewReader(ar)
	entries := make([]Entry, count)
	for i, _ := range entries {
		buf := make([]byte, entrySize)
		_, err = fr.Read(buf)
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}

		var e Entry
		r := rBuf(buf)
		e.sizeCompressed = r.Uint64()
		e.Size = r.Uint64()
		e.index = r.Uint64()
		e.adler = r.Uint32()
		e.Perm = r.Uint16()
		nlen := r.Uint16()

		sbuf := make([]byte, nlen)
		_, err = fr.Read(sbuf)
		switch {
		case err == io.EOF && i == int(count-1):
			break
		case err == io.EOF:
			return nil, io.ErrUnexpectedEOF
		case err != nil:
			return nil, err
		}
		e.Name = string(sbuf)

		entries[i] = e
	}

	if ar.Adler() != adler {
		println(ar.Adler())
		println("adler:", adler)
		return nil, ErrInvalidChecksum
	}

	return &Reader{entries, r}, nil
}

func (br *Reader) EntryReader(e *Entry) (io.ReadCloser, error) {
	_, err := br.r.Seek(int64(e.index), io.SeekStart)
	if err != nil {
		return nil, err
	}

	ar := newAdlerReader(br.r)
	fr := flate.NewReader(ar)
	return &entryReader{ar, fr, int64(e.Size), e.adler, nil}, nil
}

type entryReader struct {
	ar    *adlerReader
	r     io.Reader
	count int64
	adler uint32
	err   error
}

func (er *entryReader) Read(b []byte) (n int, err error) {
	if er.err != nil {
		n = 0
		err = er.err
		return
	}

	if int64(len(b)) > er.count {
		b = b[:er.count]
	}

	if len(b) > 0 {
		n, err = er.r.Read(b)
		er.count -= int64(n)
	}

	switch {
	case err == io.EOF && er.count > 0:
		er.err = io.ErrUnexpectedEOF
		return n, er.err
	case err == nil && er.count == 0:
		er.err = io.EOF
		return n, er.err
	default:
		er.err = err
		return n, er.err
	}
}

func (er *entryReader) Close() error {
	if er.adler != er.ar.Adler() {
		return ErrInvalidChecksum
	}
	return nil
}

type adlerReader struct {
	r     *bufio.Reader
	adler hash.Hash32
}

func newAdlerReader(r io.Reader) *adlerReader {
	br := bufio.NewReader(r)
	return &adlerReader{br, adler32.New()}
}

func (ar *adlerReader) Read(b []byte) (int, error) {
	r := io.TeeReader(ar.r, ar.adler)
	n, err := r.Read(b)
	return n, err
}

func (ar *adlerReader) ReadByte() (byte, error) {
	b, err := ar.r.ReadByte()
	if err != nil {
		return b, err
	}
	buf := []byte{b}
	ar.adler.Write(buf)
	return b, err
}

func (ar *adlerReader) Adler() uint32 {
	return ar.adler.Sum32()
}

type rBuf []byte

func (wb *rBuf) Uint8() (u uint8) {
	u = (*wb)[0]
	*wb = (*wb)[1:]
	return
}

func (wb *rBuf) Uint16() (u uint16) {
	u = binary.LittleEndian.Uint16(*wb)
	*wb = (*wb)[2:]
	return
}

func (wb *rBuf) Uint32() (u uint32) {
	u = binary.LittleEndian.Uint32(*wb)
	*wb = (*wb)[4:]
	return
}

func (wb *rBuf) Uint64() (u uint64) {
	u = binary.LittleEndian.Uint64(*wb)
	*wb = (*wb)[8:]
	return
}
