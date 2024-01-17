// Package bar implements reading and writing of BAR files.
package bar

const (
	Version = 1

	headerSize = 4
	entrySize  = 32
	footerSize = 16
)

var (
	magicNumber = []byte{'B', 'A', 'R'}
)

type Entry struct {
	Name           string
	Size           uint64
	Perm           uint16
	sizeCompressed uint64
	index          uint64
	adler          uint32
}

func (e *Entry) Ratio() float64 {
	return float64(e.sizeCompressed) / float64(e.Size)
}
