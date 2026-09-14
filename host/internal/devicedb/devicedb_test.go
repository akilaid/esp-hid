package devicedb

import (
	"strings"
	"testing"
)

func TestEmbeddedTablesParse(t *testing.T) {
	if err := Err(); err != nil {
		t.Fatalf("embedded tables: %v", err)
	}
	all := All()
	if len(all) < 10000 {
		t.Fatalf("only %d devices loaded; the generated table looks truncated", len(all))
	}
	// Same bounds config.Validate applies: an entry outside them could never
	// be selected, so it is a bad line in the file.
	for _, d := range all {
		if d.Width < 320 || d.Height > 10000 || d.Width > d.Height {
			t.Errorf("%s: size %dx%d is not a sane portrait size", d.Label(), d.Width, d.Height)
		}
		if d.Brand == "" || d.Name == "" {
			t.Errorf("entry %+v has an empty brand or name", d)
		}
	}
}

func TestAllIsSorted(t *testing.T) {
	all := All()
	for i := 1; i < len(all); i++ {
		a := strings.ToLower(all[i-1].Brand + " " + all[i-1].Name)
		b := strings.ToLower(all[i].Brand + " " + all[i].Name)
		if a > b {
			t.Fatalf("not sorted at %d: %q after %q", i, all[i].Label(), all[i-1].Label())
		}
	}
}

func TestKnownDevices(t *testing.T) {
	// One from each file, so a regenerate that drops a file is caught.
	cases := []struct {
		query  string
		brand  string
		name   string
		width  int
		height int
	}{
		{"pixel 8 pro", "Google", "Pixel 8 Pro", 1344, 2992},
		{"galaxy s24 ultra", "Samsung", "Galaxy S24 Ultra", 1440, 3120},
		{"iphone 16 pro", "Apple", "iPhone 16 Pro", 1206, 2622},
	}
	for _, tc := range cases {
		found := false
		for _, d := range Search(tc.query, 50) {
			if d.Brand == tc.brand && d.Name == tc.name {
				found = true
				if d.Width != tc.width || d.Height != tc.height {
					t.Errorf("%s: got %s, want %dx%d", tc.name, d.Resolution(), tc.width, tc.height)
				}
			}
		}
		if !found {
			t.Errorf("Search(%q) did not return %s %s", tc.query, tc.brand, tc.name)
		}
	}
}

func TestSearchRequiresEveryToken(t *testing.T) {
	for _, d := range Search("pixel 8 pro", 200) {
		key := strings.ToLower(d.Brand + " " + d.Name)
		for _, token := range []string{"pixel", "8", "pro"} {
			if !strings.Contains(key, token) {
				t.Errorf("%q matched without token %q", d.Label(), token)
			}
		}
	}
}

func TestSearchRanksExactNamePrefixFirst(t *testing.T) {
	got := Search("iphone 16", 50)
	if len(got) == 0 {
		t.Fatal("no results")
	}
	if !strings.HasPrefix(strings.ToLower(got[0].Name), "iphone 16") {
		t.Errorf("first result %q does not start with the query", got[0].Label())
	}
	// Everything whose name starts with the phrase precedes everything that
	// merely contains it.
	seenOther := false
	for _, d := range got {
		starts := strings.HasPrefix(strings.ToLower(d.Name), "iphone 16")
		if !starts {
			seenOther = true
		} else if seenOther {
			t.Fatalf("%q (a prefix match) came after a non-prefix match", d.Label())
		}
	}
}

func TestSearchIsCaseInsensitiveAndTrimmed(t *testing.T) {
	a := Search("  Galaxy   S24  ", 20)
	b := Search("galaxy s24", 20)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("got %d and %d results for the same query", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("result %d differs: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestSearchLimitAndEmptyQuery(t *testing.T) {
	if got := Search("", 50); got != nil {
		t.Errorf("empty query returned %d results", len(got))
	}
	if got := Search("   ", 50); got != nil {
		t.Errorf("blank query returned %d results", len(got))
	}
	if got := Search("galaxy", 5); len(got) != 5 {
		t.Errorf("limit 5 returned %d", len(got))
	}
	if got := Search("galaxy", 0); got != nil {
		t.Errorf("limit 0 returned %d results", len(got))
	}
}

func TestLabelAndResolution(t *testing.T) {
	d := Device{Brand: "Samsung", Name: "Galaxy S24 Ultra", Width: 1440, Height: 3120}
	if got := d.Resolution(); got != "1440x3120" {
		t.Errorf("Resolution = %q", got)
	}
	if got := d.Label(); got != "Samsung Galaxy S24 Ultra (1440x3120)" {
		t.Errorf("Label = %q", got)
	}
}

func TestParseTSVRejectsMalformedLines(t *testing.T) {
	if _, err := parseTSV("Apple\tiPhone\t1170\n"); err == nil {
		t.Error("three fields accepted")
	}
	if _, err := parseTSV("Apple\tiPhone\tabc\t2532\n"); err == nil {
		t.Error("non-numeric width accepted")
	}
	got, err := parseTSV("# comment\n\nApple\tiPhone\t1170\t2532\r\n")
	if err != nil || len(got) != 1 || got[0].Width != 1170 {
		t.Errorf("comment/blank/CRLF handling: %v, %+v", err, got)
	}
}
