// Package devicedb is the built-in table behind the device picker: type a
// model name, get the screen's portrait pixel size to seed the resolution
// field with.
//
// The table is two embedded TSV files. devices.tsv is generated (see gen/)
// and never edited by hand; apple.tsv is small enough to maintain directly.
// Both are plain text so a change shows up in a diff.
//
// The sizes are the panel's pixels, which is what the virtual-cursor model
// wants in the common case. They are a starting point, not the truth: some
// phones render below their panel (a QHD+ Samsung defaults to FHD+), and a
// foldable is listed by its inner display. The resolution field stays
// editable for exactly that reason.
package devicedb

import (
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:generate go run ./gen -in ../../../devices.csv -out devices.tsv

//go:embed devices.tsv
var devicesTSV string

//go:embed apple.tsv
var appleTSV string

// Device is one row of the table. Width and Height are portrait: Width is
// the short side. DPI is the pixel density (an Android bucket such as 420,
// or Apple's ppi), 0 when the export did not say; it sizes the device in the
// arrangement picture and plays no part in switching.
type Device struct {
	Brand  string
	Name   string
	Width  int
	Height int
	DPI    int
}

// Resolution renders the size the way config.ParseResolution reads it.
func (d Device) Resolution() string {
	return fmt.Sprintf("%dx%d", d.Width, d.Height)
}

// Label is the picker's display text. The size is part of it because one
// model name can appear with several sizes (regional variants), and the
// number is what the user is actually choosing.
func (d Device) Label() string {
	return d.Brand + " " + d.Name + " (" + d.Resolution() + ")"
}

// entry is a Device with its search key precomputed, so typing in the
// picker does not lower-case twenty thousand strings per keystroke.
type entry struct {
	Device
	name string // lower-cased Name
	key  string // lower-cased "Brand Name"
}

var (
	loadOnce sync.Once
	entries  []entry
	loadErr  error
)

func load() {
	loadOnce.Do(func() {
		for _, src := range []string{devicesTSV, appleTSV} {
			parsed, err := parseTSV(src)
			if err != nil {
				loadErr = err
				return
			}
			entries = append(entries, parsed...)
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].key != entries[j].key {
				return entries[i].key < entries[j].key
			}
			if entries[i].Width != entries[j].Width {
				return entries[i].Width < entries[j].Width
			}
			return entries[i].Height < entries[j].Height
		})
	})
}

// parseTSV reads "brand\tname\twidth\theight\tdpi" lines; blank lines and
// lines starting with '#' are skipped. A malformed line is an error rather
// than a silent skip: the files are checked in, so a bad line is a bug to
// fix, not data to tolerate.
func parseTSV(src string) ([]entry, error) {
	var out []entry
	for n, line := range strings.Split(src, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return nil, fmt.Errorf("line %d: want 5 tab-separated fields, got %d", n+1, len(fields))
		}
		width, err1 := strconv.Atoi(fields[2])
		height, err2 := strconv.Atoi(fields[3])
		dpi, err3 := strconv.Atoi(fields[4])
		if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
			return nil, fmt.Errorf("line %d: bad size %q x %q", n+1, fields[2], fields[3])
		}
		if err3 != nil || dpi < 0 {
			return nil, fmt.Errorf("line %d: bad density %q", n+1, fields[4])
		}
		d := Device{Brand: fields[0], Name: fields[1], Width: width, Height: height, DPI: dpi}
		out = append(out, entry{
			Device: d,
			name:   strings.ToLower(d.Name),
			key:    strings.ToLower(d.Brand + " " + d.Name),
		})
	}
	return out, nil
}

// All returns every device, sorted by brand then name. The slice is shared;
// treat it as read-only.
func All() []Device {
	load()
	out := make([]Device, len(entries))
	for i := range entries {
		out[i] = entries[i].Device
	}
	return out
}

// Err reports a problem parsing the embedded tables. It is nil in any build
// whose tests pass; it exists so a GUI can log rather than show an empty
// picker with no explanation.
func Err() error {
	load()
	return loadErr
}

// Search matches every whitespace-separated token of query, case-
// insensitively, against "Brand Name", and returns at most limit devices.
// Ranking is by how the query sits in the name: models whose name starts
// with the query come first (typing "pixel 8" should lead with Pixel 8, not
// Pixel 8 Pro's cousins), then names that start with the first token, then
// everything else — alphabetical within each tier. An empty query matches
// nothing: the picker should sit empty until the user types.
func Search(query string, limit int) []Device {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 || limit <= 0 {
		return nil
	}
	load()
	phrase := strings.Join(tokens, " ")

	var tiers [3][]Device
	for i := range entries {
		e := &entries[i]
		if !containsAll(e.key, tokens) {
			continue
		}
		switch {
		case strings.HasPrefix(e.name, phrase):
			tiers[0] = append(tiers[0], e.Device)
		case strings.HasPrefix(e.name, tokens[0]):
			tiers[1] = append(tiers[1], e.Device)
		default:
			tiers[2] = append(tiers[2], e.Device)
		}
	}

	var out []Device
	for _, tier := range tiers {
		for _, d := range tier {
			if len(out) == limit {
				return out
			}
			out = append(out, d)
		}
	}
	return out
}

// DensityFor is the typical pixel density of a screen of this size: the
// median DPI over every table entry with these dimensions, either way up.
// It is what the arrangement picture uses for a resolution typed by hand or
// restored from settings, where no particular device is known; 0 when no
// entry has that size.
func DensityFor(width, height int) int {
	if width > height {
		width, height = height, width
	}
	load()
	var dpis []int
	for i := range entries {
		e := &entries[i]
		if e.Width == width && e.Height == height && e.DPI > 0 {
			dpis = append(dpis, e.DPI)
		}
	}
	if len(dpis) == 0 {
		return 0
	}
	sort.Ints(dpis)
	return dpis[len(dpis)/2]
}

func containsAll(haystack string, tokens []string) bool {
	for _, t := range tokens {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}
