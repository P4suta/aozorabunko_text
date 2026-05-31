// Command aozora builds a human-readable, UTF-8 corpus from an Aozora Bunko
// source checkout (https://github.com/aozorabunko/aozorabunko).
//
// For each work zip under <src>/cards it looks up the author and title in the
// Aozora metadata CSV (list_person_all_extended_utf8.csv), decodes the
// Shift_JIS text to UTF-8, and writes it to a readable path:
//
//	作品/<著者名>/<作品名>.txt
//
// Works whose metadata can't be matched go to _unmatched/, and texts that fail
// Shift_JIS decoding go to _decode_errors/ — both quarantined and reported with
// counts, so nothing is dropped or corrupted silently.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

type meta struct {
	author string // 姓+名
	title  string // 作品名
	kind   string // 文字遣い種別 (used to disambiguate same-title variants)
}

type workKey struct{ person, work int }

type job struct {
	zipPath string
	person  int
	zipbase string
	matched bool
	author  string
	title   string
	kind    string
	label   string // filename stem (title, possibly disambiguated)
	outRel  string // final path relative to dest
}

func main() {
	src := flag.String("src", "aozorabunko", "source repository dir containing cards/")
	dest := flag.String("dest", ".", "destination dir to write the corpus into")
	metaPath := flag.String("meta", "", "path to list_person_all_extended_utf8.csv")
	workers := flag.Int("workers", runtime.NumCPU(), "number of concurrent workers")
	flag.Parse()

	byTail := map[string]meta{}
	byWork := map[workKey]meta{}
	if *metaPath != "" {
		if err := loadMeta(*metaPath, byTail, byWork); err != nil {
			log.Fatalf("load metadata %s: %v", *metaPath, err)
		}
		log.Printf("metadata: %d works indexed by URL, %d by id", len(byTail), len(byWork))
	} else {
		log.Print("WARNING: no -meta given; every work will land in _unmatched/")
	}

	jobs := collectJobs(*src, byTail, byWork)
	log.Printf("found %d work zips under %s/cards", len(jobs), *src)
	resolveCollisions(jobs)

	var works, unmatched, decodeErr, skipped, noTxt, failed int64
	sem := make(chan struct{}, *workers)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j *job) {
			defer wg.Done()
			defer func() { <-sem }()
			switch process(j, *dest) {
			case statusWork:
				atomic.AddInt64(&works, 1)
			case statusUnmatched:
				atomic.AddInt64(&unmatched, 1)
			case statusDecodeErr:
				atomic.AddInt64(&decodeErr, 1)
			case statusSkip:
				atomic.AddInt64(&skipped, 1)
			case statusNoTxt:
				atomic.AddInt64(&noTxt, 1)
			case statusFailed:
				atomic.AddInt64(&failed, 1)
			}
		}(j)
	}
	wg.Wait()

	log.Printf("done: %d works, %d unmatched, %d decode-errors, %d skipped, %d no-txt, %d failed",
		works, unmatched, decodeErr, skipped, noTxt, failed)
	if decodeErr > 0 {
		log.Printf("NOTE: %d file(s) had Shift_JIS decode issues; see _decode_errors/", decodeErr)
	}
	if unmatched > 0 {
		log.Printf("NOTE: %d file(s) had no metadata match; see _unmatched/", unmatched)
	}
}

// stripBOM removes a leading UTF-8 byte-order mark (U+FEFF) if present.
func stripBOM(s string) string {
	if r, size := utf8.DecodeRuneInString(s); r == 0xFEFF {
		return s[size:]
	}
	return s
}

// loadMeta reads the Aozora metadata CSV and fills two indexes: by the
// "cards/<id>/files/<name>.zip" tail of each text-file URL (exact), and by
// (person-id, work-id) (catches version variants not listed by URL).
func loadMeta(path string, byTail map[string]meta, byWork map[workKey]meta) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(stripBOM(h))] = i
	}
	must := func(name string) int {
		c, ok := idx[name]
		if !ok {
			log.Fatalf("metadata CSV missing expected column %q", name)
		}
		return c
	}
	cPerson := must("人物ID")
	cWork := must("作品ID")
	cTitle := must("作品名")
	cURL := must("テキストファイルURL")
	cSei := idx["姓"]
	cMei := idx["名"]
	cKind := idx["文字遣い種別"]
	at := func(rec []string, i int) string {
		if i >= 0 && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // tolerate the occasional malformed row
		}
		m := meta{
			author: at(rec, cSei) + at(rec, cMei),
			title:  at(rec, cTitle),
			kind:   at(rec, cKind),
		}
		if tail := cardsTail(at(rec, cURL)); tail != "" {
			byTail[tail] = m
		}
		person := atoiSafe(at(rec, cPerson))
		work := atoiSafe(at(rec, cWork))
		if person >= 0 && work >= 0 {
			if _, exists := byWork[workKey{person, work}]; !exists {
				byWork[workKey{person, work}] = m
			}
		}
	}
	return nil
}

func collectJobs(src string, byTail map[string]meta, byWork map[workKey]meta) []*job {
	root := filepath.Join(src, "cards")
	var jobs []*job
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel) // cards/000005/files/53194_ruby_44732.zip
		person := -1
		if parts := strings.Split(relSlash, "/"); len(parts) >= 2 {
			person = atoiSafe(parts[1])
		}
		zipbase := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		j := &job{zipPath: p, person: person, zipbase: zipbase}
		if m, ok := byTail[relSlash]; ok {
			j.matched, j.author, j.title, j.kind = true, m.author, m.title, m.kind
		} else if m, ok := byWork[workKey{person, atoiSafe(leadingDigits(zipbase))}]; ok {
			j.matched, j.author, j.title, j.kind = true, m.author, m.title, m.kind
		}
		jobs = append(jobs, j)
		return nil
	})
	if err != nil {
		log.Fatalf("walk %s: %v", root, err)
	}
	return jobs
}

// resolveCollisions assigns each job its final output path, disambiguating
// multiple files that map to the same 作品/著者/作品名.txt.
func resolveCollisions(jobs []*job) {
	groups := map[string][]*job{}
	for _, j := range jobs {
		if j.matched {
			groups[j.author+"\x00"+j.title] = append(groups[j.author+"\x00"+j.title], j)
		}
	}
	for _, g := range groups {
		if len(g) == 1 {
			g[0].label = g[0].title
			continue
		}
		sort.Slice(g, func(a, b int) bool { return g[a].zipbase < g[b].zipbase })
		// Prefer the human-readable 文字遣い種別 as the disambiguator; fall back
		// to the (opaque but unique) zip basename if that isn't distinguishing.
		seen := map[string]bool{}
		useKind := true
		for _, j := range g {
			if j.kind == "" || seen[j.kind] {
				useKind = false
				break
			}
			seen[j.kind] = true
		}
		for _, j := range g {
			if useKind {
				j.label = j.title + "（" + j.kind + "）"
			} else {
				j.label = j.title + "（" + j.zipbase + "）"
			}
		}
	}
	for _, j := range jobs {
		if j.matched {
			author := sanitize(j.author)
			if author == "" {
				author = "著者不明"
			}
			label := sanitize(j.label)
			if label == "" {
				label = j.zipbase
			}
			j.outRel = filepath.Join("作品", author, label+".txt")
		} else {
			j.outRel = filepath.Join("_unmatched", fmt.Sprintf("%06d", max0(j.person)), j.zipbase+".txt")
		}
	}
}

type status int

const (
	statusWork status = iota
	statusUnmatched
	statusDecodeErr
	statusSkip
	statusNoTxt
	statusFailed
)

func process(j *job, dest string) status {
	planned := filepath.Join(dest, j.outRel)
	if fileExists(planned) {
		return statusSkip
	}

	r, err := zip.OpenReader(j.zipPath)
	if err != nil {
		log.Printf("open %s: %v", j.zipPath, err)
		return statusFailed
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
		return statusNoTxt
	}

	rc, err := entry.Open()
	if err != nil {
		log.Printf("read %s: %v", j.zipPath, err)
		return statusFailed
	}
	raw, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		log.Printf("read %s: %v", j.zipPath, err)
		return statusFailed
	}

	// Shift_JIS -> UTF-8. The decoder is lenient, so a U+FFFD in the output
	// means unmappable bytes; treat that (or a transform error) as a failure
	// and quarantine rather than publishing mojibake into the clean tree.
	utf8b, _, derr := transform.Bytes(japanese.ShiftJIS.NewDecoder(), raw)
	bad := derr != nil || bytes.ContainsRune(utf8b, utf8.RuneError)

	out, st := planned, statusWork
	if !j.matched {
		st = statusUnmatched
	}
	if bad {
		out = filepath.Join(dest, "_decode_errors", fmt.Sprintf("%06d", max0(j.person)), j.zipbase+".txt")
		st = statusDecodeErr
		log.Printf("decode error in %s -> quarantined to _decode_errors/", j.zipPath)
	}
	if err := writeFile(out, utf8b); err != nil {
		log.Printf("write %s: %v", out, err)
		return statusFailed
	}
	return st
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// sanitize makes a string safe as a single path component: forbidden ASCII
// path characters become their full-width (and still readable) equivalents,
// control characters are dropped, and the result is length-capped.
var fullWidth = strings.NewReplacer(
	"/", "／", `\`, "＼", ":", "：", "*", "＊",
	"?", "？", `"`, "”", "<", "＜", ">", "＞", "|", "｜",
)

func sanitize(s string) string {
	s = fullWidth.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	s = strings.TrimSpace(b.String())
	s = strings.Trim(s, ". 　")
	s = strings.TrimSpace(s)
	if rs := []rune(s); len(rs) > 120 {
		s = string(rs[:120])
	}
	return s
}

func cardsTail(url string) string {
	if i := strings.Index(url, "cards/"); i >= 0 {
		return url[i:]
	}
	return ""
}

func leadingDigits(s string) string {
	for i, r := range s {
		if r < '0' || r > '9' {
			return s[:i]
		}
	}
	return s
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimLeft(s, "0"))
	if err != nil {
		if s != "" && strings.Trim(s, "0") == "" {
			return 0 // all zeros
		}
		return -1
	}
	return n
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
