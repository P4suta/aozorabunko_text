// Command extzip extracts the first .txt entry from each Aozora Bunko zip
// under <src>/cards and writes it into <dest>/cards, mirroring the layout:
//
//	cards/000005/files/53194_ruby_44732.zip
//	  -> cards/000005/files/53194_ruby_44732/53194_ruby_44732.txt
//
// The text is copied as raw bytes (the originals are Shift_JIS); no charset
// conversion is performed. Existing output files are skipped, so re-runs are
// incremental. Uses only the standard library — no external dependencies.
package main

import (
	"archive/zip"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	errSkip  = errors.New("output already exists")
	errNoTxt = errors.New("no .txt entry in zip")
)

func main() {
	src := flag.String("src", "aozorabunko", "source repository dir containing cards/")
	dest := flag.String("dest", ".", "destination dir to write cards/ into")
	workers := flag.Int("workers", runtime.NumCPU(), "number of concurrent workers")
	flag.Parse()

	cardsRoot := filepath.Join(*src, "cards")
	var zips []string
	err := filepath.WalkDir(cardsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
			zips = append(zips, path)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("walk %s: %v", cardsRoot, err)
	}
	log.Printf("found %d zip files under %s", len(zips), cardsRoot)

	var extracted, skipped, noTxt, failed int64
	sem := make(chan struct{}, *workers)
	var wg sync.WaitGroup
	for _, zp := range zips {
		wg.Add(1)
		sem <- struct{}{}
		go func(zp string) {
			defer wg.Done()
			defer func() { <-sem }()
			switch err := extract(zp, *src, *dest); {
			case errors.Is(err, errSkip):
				atomic.AddInt64(&skipped, 1)
			case errors.Is(err, errNoTxt):
				atomic.AddInt64(&noTxt, 1)
			case err != nil:
				atomic.AddInt64(&failed, 1)
				log.Printf("skip %s: %v", zp, err)
			default:
				atomic.AddInt64(&extracted, 1)
			}
		}(zp)
	}
	wg.Wait()
	log.Printf("done: %d extracted, %d skipped, %d without-txt, %d failed",
		extracted, skipped, noTxt, failed)
}

// extract reads zipPath, finds the first *.txt entry and writes it to
// <dest>/<relDir>/<base>/<base>.txt where relDir/base mirror zipPath's
// location relative to src.
func extract(zipPath, src, dest string) error {
	rel, err := filepath.Rel(src, zipPath)
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)) // name without ".zip"
	outDir := filepath.Join(dest, filepath.Dir(rel), base)
	outFile := filepath.Join(outDir, base+".txt")
	if _, err := os.Stat(outFile); err == nil {
		return errSkip
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	var entry *zip.File
	for _, f := range r.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".txt") {
			entry = f
			break
		}
	}
	if entry == nil {
		return errNoTxt
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	rc, err := entry.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	// Write to a temp file first, then rename, so an interrupted run never
	// leaves a half-written .txt that a later run would skip as "done".
	tmp := outFile + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, outFile)
}
