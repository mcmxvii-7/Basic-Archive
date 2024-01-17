package bar

import (
	"compress/flate"
	"encoding/binary"
	"errors"
	"hash"
	"hash/adler32"
	"io"
	"path/filepath"
	"strings"
)

var (
	ErrNoValidEntry    = errors.New("No valid entry to write")
	ErrPathIsNotSimple = errors.New("Filepath is not simple")
	ErrWriteAfterClose = errors.New("Write after close")
)

type Writer struct {
	w       io.Writer
	index   uint64
	entries []Entry
	curr    *dataWriter
	err     error
}

func NewWriter(w io.Writer) (*Writer, error) {
	header := make([]byte, headerSize)
	copy(header[0:3], magicNumber)
	header[3] = Version

	n, err := w.Write(header)
	if err != nil {
		return nil, err
	}

	return &Writer{w, uint64(n), nil, nil, ErrNoValidEntry}, nil
}

func (bw *Writer) Create(name string) error {
	if bw.err != nil && bw.err != ErrNoValidEntry {
		return bw.err
	}

	if bw.err != ErrNoValidEntry {
		err := bw.finalizeEntry()
		if err != nil {
			bw.err = err
			return err
		}
	}
	bw.err = nil

	if !filepath.IsLocal(name) || strings.IndexRune(name, '\\') != -1 {
		bw.err = ErrPathIsNotSimple
		return bw.err
	}

	var e Entry
	e.Name = name
	e.Perm = 0644
	e.index = uint64(bw.index)

	bw.entries = append(bw.entries, e)
	var err error
	bw.curr, err = newDataWriter(bw.w)
	if err != nil {
		bw.err = err
		return err
	}

	return nil
}

func (bw *Writer) SetPerms(perm uint16) error {
	if bw.err != nil {
		return bw.err
	}

	bw.entries[len(bw.entries)-1].Perm = perm
	return nil
}

func (bw *Writer) Write(p []byte) (int, error) {
	if bw.err != nil {
		return 0, bw.err
	}

	n, err := bw.curr.Write(p)
	if err != nil {
		bw.err = err
	}
	return n, err
}

func (bw *Writer) Close() error {
	if bw.err != nil {
		return bw.err
	}

	adler, err := bw.writeTable()
	if err != nil {
		bw.err = err
		return err
	}

	buf := make([]byte, footerSize)
	wb := wBuf(buf)
	wb.Uint64(bw.index)
	wb.Uint32(adler)
	wb.Uint32(uint32(len(bw.entries)))

	_, err = bw.w.Write(buf)
	if err != nil {
		bw.err = err
		return err
	}
	bw.err = ErrWriteAfterClose

	return nil
}

func (bw *Writer) writeTable() (uint32, error) {
	err := bw.finalizeEntry()
	if err != nil {
		return 0, err
	}

	w, err := newDataWriter(bw.w)
	if err != nil {
		return 0, err
	}

	for _, x := range bw.entries {
		buf := make([]byte, entrySize)
		wb := wBuf(buf)
		wb.Uint64(x.sizeCompressed)
		wb.Uint64(x.Size)
		wb.Uint64(x.index)
		wb.Uint32(x.adler)
		wb.Uint16(x.Perm)
		wb.Uint16(uint16(len(x.Name)))

		_, err := w.Write(buf)
		if err != nil {
			return 0, err
		}

		_, err = io.WriteString(w, x.Name)
		if err != nil {
			return 0, err
		}
	}

	err = w.Close()
	if err != nil {
		return 0, err
	}

	return w.Adler(), nil
}

func (bw *Writer) finalizeEntry() error {
	if err := bw.curr.Close(); err != nil {
		return err
	}

	bw.index += bw.curr.CompressedCount()

	i := len(bw.entries) - 1
	bw.entries[i].sizeCompressed = bw.curr.CompressedCount()
	bw.entries[i].adler = bw.curr.Adler()
	bw.entries[i].Size = bw.curr.UncompressedCount()

	bw.curr = nil
	return nil
}

type adlerWriter struct {
	w     io.Writer
	adler hash.Hash32
}

func newAdlerWriter(w io.Writer) *adlerWriter {
	return &adlerWriter{w, adler32.New()}
}

func (aw *adlerWriter) Sum32() uint32 {
	return aw.adler.Sum32()
}

func (aw *adlerWriter) Write(p []byte) (int, error) {
	w := io.MultiWriter(aw.adler, aw.w)
	n, err := w.Write(p)
	return n, err
}

type countWriter struct {
	w     io.Writer
	count uint64
}

func newCountWriter(w io.Writer) *countWriter {
	return &countWriter{w, 0}
}

func (cw *countWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	if err != nil {
		return 0, err
	}
	cw.count += uint64(n)
	return n, err
}

type dataWriter struct {
	uncompCounter *countWriter
	compCounter   *countWriter
	adler         *adlerWriter
	deflate       io.WriteCloser
}

func newDataWriter(w io.Writer) (*dataWriter, error) {
	var dw dataWriter
	dw.adler = newAdlerWriter(w)
	dw.compCounter = newCountWriter(dw.adler)
	var err error
	dw.deflate, err = flate.NewWriter(dw.compCounter, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	dw.uncompCounter = newCountWriter(dw.deflate)
	return &dw, nil
}

func (dw *dataWriter) Write(p []byte) (int, error) {
	n, err := dw.uncompCounter.Write(p)
	return n, err
}

func (dw *dataWriter) Close() error {
	return dw.deflate.Close()
}

func (dw *dataWriter) CompressedCount() uint64 {
	return dw.compCounter.count
}

func (dw *dataWriter) UncompressedCount() uint64 {
	return dw.uncompCounter.count
}

func (dw *dataWriter) Adler() uint32 {
	return dw.adler.Sum32()
}

type wBuf []byte

func (wb *wBuf) Uint8(u uint8) {
	(*wb)[0] = u
	*wb = (*wb)[1:]
}

func (wb *wBuf) Uint16(u uint16) {
	binary.LittleEndian.PutUint16(*wb, u)
	*wb = (*wb)[2:]
}

func (wb *wBuf) Uint32(u uint32) {
	binary.LittleEndian.PutUint32(*wb, u)
	*wb = (*wb)[4:]
}

func (wb *wBuf) Uint64(u uint64) {
	binary.LittleEndian.PutUint64(*wb, u)
	*wb = (*wb)[8:]
}
