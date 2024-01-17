package main

import (
	"bar/archive/bar"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
)

var (
	versionFlag  = flag.Bool("v", false, "Print version.")
	listFlag     = flag.Bool("l", false, "List names.")
	extractFlag  = flag.Bool("x", false, "Extract files.")
	overrideFlag = flag.Bool("o", false, "Override file.")
	nameFlag     = flag.String("n", "", "Name of the file.")

	files = make(map[string]FileInfo)
	warn  = log.New(os.Stderr, "Warning: ", 0)

	errDuplicateFilename   = errors.New("Duplicate filename.")
	errUnsupportedFiletype = errors.New("Unsupported file type.")
)

type FileInfo struct {
	Path string
	Perm uint16
}

func init() {
	log.SetFlags(0)
	log.SetPrefix("Error: ")
}

func main() {
	flag.Parse()
	args := flag.Args()

	switch {
	case *versionFlag:
		fmt.Printf("version: %d\n", bar.Version)
	case *listFlag && *extractFlag:
		log.Fatalf("Conflictnig flags '-l' and '-x'.\n")
	case *listFlag:
		list(args)
	case *extractFlag:
		extract(args)
	default:
		create(args)
	}
}

func list(args []string) {
	if *nameFlag != "" {
		log.Println("Conflicting flag '-n'\n")
		return
	}

	if *overrideFlag != false {
		log.Println("Conflicting flag '-o'\n")
		return
	}

	if len(args) != 1 {
		log.Println("Invalid number of arguments.")
		return
	}

	filename := args[0]

	_, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("No such file '%s'.\n", filename)
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		log.Printf("Unable to read file '%s'.\n", filename)
		return
	}
	defer file.Close()

	r, err := bar.NewReader(file)
	switch {
	case err == bar.ErrUnknownFormat:
		log.Printf("Unknown file format.\n")
		return
	case err == bar.ErrUnsupportedVersion:
		log.Printf("Unsupported version.\n")
		return
	case err == bar.ErrInvalidChecksum:
		log.Printf("Invalid checksum.\n")
		return
	case err != nil:
		log.Printf("Unable to read file '%s'.", filename)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, e := range r.Entries {
		fmt.Fprintf(w, "%s\t0%o\t%.2f%%\n", e.Name, e.Perm, e.Ratio()*100)
	}
	w.Flush()
}

func extract(args []string) {
	if len(args) != 1 {
		log.Printf("Invalid number of arguments.\n")
		return
	}

	filename := args[0]

	file, err := os.Open(filename)
	if err != nil {
		log.Printf("Unable to read file '%s'.\n", filename)
		return
	}
	defer file.Close()

	r, err := bar.NewReader(file)
	switch {
	case err == bar.ErrUnknownFormat:
		log.Printf("Unknown file format.\n")
		return
	case err == bar.ErrUnsupportedVersion:
		log.Printf("Unsupported version.\n")
		return
	case err != nil:
		log.Printf("Unable to read file '%s'.", filename)
		return
	}

	if *nameFlag == "" {
		extractEntries(r, r.Entries)
	} else {
		name := *nameFlag
		cmp := func(r bar.Entry) bool { return r.Name == name }
		if i := slices.IndexFunc(r.Entries, cmp); i != -1 {
			es := []bar.Entry{r.Entries[i]}
			extractEntries(r, es[:])
		} else {
			log.Printf("No such file '%s' in archive.\n", name)
			return
		}
	}
}

func extractEntries(r *bar.Reader, entries []bar.Entry) {
	for _, e := range entries {
		s, err := os.Stat(e.Name)
		if err == nil {
			if *overrideFlag {
				if s.IsDir() {
					log.Printf("Unable to override. '%s' is a directory.\n",
						e.Name)
					return
				}
				warn.Printf("Overriding file '%s'.\n", e.Name)
			} else {
				log.Printf("File '%s' allready exists.\n", e.Name)
				return
			}
		}
	}

	for i, e := range entries {
		err := os.MkdirAll(filepath.Dir(e.Name), 0755)
		if err != nil {
			log.Printf("Unable to create file '%s'.\n", e.Name)
			return
		}
		flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		file, err := os.OpenFile(e.Name, flags, fs.FileMode(e.Perm))
		if err != nil {
			log.Printf("Unable to create file '%s'.\n", e.Name)
			return
		}

		er, err := r.EntryReader(&entries[i])
		if err != nil {
			log.Printf("Unable to create file '%s'.\n", e.Name)
			return
		}

		_, err = io.Copy(file, er)
		if err != nil {
			log.Printf("Unable to write file '%s'.\n", e.Name)
			return
		}
		err = er.Close()
		if err == bar.ErrInvalidChecksum {
			warn.Printf("Invalid checksum for file '%s'.\n", e.Name)
		}

		file.Close()
	}
}

func create(args []string) {
	if *nameFlag != "" {
		log.Printf("Conflicting flag '-n'\n")
		return
	}

	if len(args) < 2 {
		log.Printf("Invalid number of arguments.\n")
		return
	}

	var (
		outFile    = args[0]
		inputFiles = args[1:]
	)

	_, err := os.Stat(outFile)
	if err == nil {
		if *overrideFlag {
			warn.Printf("Overriing file '%s'.\n", outFile)
		} else {
			log.Printf("File '%s' allready exits.\n", outFile)
			return
		}
	}

	err = addNames(inputFiles)
	if err != nil {
		return
	}

	file, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Printf("Unable to create file.\n")
		return
	}
	defer file.Close()

	w, err := bar.NewWriter(file)
	if err != nil {
		log.Printf("Unable to write file.\n")
		return
	}

	for name, info := range files {
		err := w.Create(name)
		if err != nil {
			log.Printf("Unable to write file.\n")
			return
		}
		w.SetPerms(info.Perm)

		ifile, err := os.Open(info.Path)
		if err != nil {
			log.Printf("Unable to read file '%s'.\n", info.Path)
			return
		}
		io.Copy(w, ifile)
		ifile.Close()
	}

	err = w.Close()
	if err != nil {
		log.Printf("Unable to write file.\n")
	}
}

func addNames(names []string) error {
	for _, e := range names {
		s, err := os.Stat(e)
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("File '%s' does not exits.", e)
			return err
		}

		if s.IsDir() {
			err := addDirectory(e)
			if err != nil {
				return err
			}
		} else if s.Mode().IsRegular() {
			err := addFile(e, uint16(s.Mode()&fs.ModePerm))
			if err != nil {
				return err
			}
		} else {
			log.Printf("'%s' is not a regular file or directory.\n", e)
			return errUnsupportedFiletype
		}
	}
	return nil
}

func addDirectory(dirname string) error {
	entries, err := os.ReadDir(dirname)
	switch {
	case errors.Is(err, os.ErrPermission):
		log.Printf("Permission denied '%s'.\n", dirname)
		fallthrough
	case err != nil:
		return err
	}

	var names []string
	for _, e := range entries {
		names = append(names, filepath.Join(dirname, e.Name()))
	}
	return addNames(names)
}

func addFile(file string, perm uint16) error {
	var (
		name string
		path string
	)

	file = filepath.Clean(file)
	file = filepath.ToSlash(file)
	if filepath.IsAbs(file) {
		warn.Printf("'%s' => '%s'\n", file, file[1:])
		path = file
		name = file[1:]
	} else {
		var err error
		path, err = filepath.Abs(file)
		if err != nil {
			log.Printf("Invalid filepath '%s'.\n", file)
			return err
		}

		name = file

		var b bool
		for strings.HasPrefix(name, "../") {
			b = true
			name = name[3:]
		}
		if b {
			warn.Printf("'%s' => '%s'\n", file, name)
		}
	}

	_, ok := files[name]
	if ok {
		log.Printf("Duplicate filename '%s' (%s).\n", name, path)
		return errDuplicateFilename
	}
	files[name] = FileInfo{path, perm}
	return nil
}
