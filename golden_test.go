package qfreportpdf_test

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/blutspende/qfreportpdf"
)

const (
	goldenRoot   = "testdata"
	templateFile = "template.json"
	dataFile     = "data.json"
	goldenFile   = "golden.pdf"

	visualTolerancePercent = 0.01
)

func TestGolden(t *testing.T) {
	for _, suite := range fixtureFolders(t, goldenRoot, true) {
		t.Run(suite, func(t *testing.T) {
			dir := filepath.Join(goldenRoot, suite)
			template := readFixture(t, filepath.Join(dir, templateFile))
			for _, name := range fixtureFolders(t, dir, true, templateFile) {
				t.Run(name, func(t *testing.T) {
					runGoldenCase(t, filepath.Join(dir, name), template)
				})
			}
		})
	}
}

func runGoldenCase(t *testing.T, dir string, template []byte) {
	t.Helper()
	fixtureFolders(t, dir, false, dataFile, goldenFile)
	data := readFixture(t, filepath.Join(dir, dataFile))
	want := readFixture(t, filepath.Join(dir, goldenFile))
	got, err := qfreportpdf.Generate(template, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(t.ArtifactDir(), "actual.pdf"), got, 0600); err != nil {
		t.Fatal(err)
	}
	comparePDF(t, want, got)
}

func fixtureFolders(t *testing.T, dir string, allowFolders bool, allowedFiles ...string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var folders []string
	for _, entry := range entries {
		if allowFolders && entry.IsDir() {
			folders = append(folders, entry.Name())
			continue
		}
		if !entry.Type().IsRegular() || !slices.Contains(allowedFiles, entry.Name()) {
			t.Errorf("unexpected fixture entry %s", filepath.Join(dir, entry.Name()))
		}
	}
	if allowFolders && len(folders) == 0 {
		t.Fatalf("expected at least one fixture folder in %s", dir)
	}
	return folders
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Supply the PDF on stdin and keep Poppler's diagnostics out of the extracted text and images.
func poppler(t *testing.T, pdf []byte, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Stdin = bytes.NewReader(pdf)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, diagnostics.String())
	}
	return out
}

type pageSize struct {
	Width  float64 `xml:"width,attr"`
	Height float64 `xml:"height,attr"`
}

func pageSizes(t *testing.T, pdf []byte) []pageSize {
	t.Helper()
	// The bounding-box output includes the dimensions in PDF points for every page.
	var doc struct {
		Pages []pageSize `xml:"body>doc>page"`
	}
	if err := xml.Unmarshal(poppler(t, pdf, "pdftotext", "-bbox", "-", "-"), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) == 0 {
		t.Fatal("Poppler reported no pages")
	}
	return doc.Pages
}

func comparePDF(t *testing.T, want, got []byte) {
	t.Helper()
	expected, actual := pageSizes(t, want), pageSizes(t, got)
	if len(actual) != len(expected) {
		t.Fatalf("page count: got %d, want %d", len(actual), len(expected))
	}
	for i, size := range expected {
		page := strconv.Itoa(i + 1)
		if actual[i] != size {
			t.Errorf("page %s dimensions in points: got %+v, want %+v", page, actual[i], size)
			continue
		}
		args := []string{"-f", page, "-l", page, "-enc", "UTF-8", "-eol", "unix", "-", "-"}
		wantText := poppler(t, want, "pdftotext", args...)
		gotText := poppler(t, got, "pdftotext", args...)
		textChanged := !bytes.Equal(wantText, gotText)
		if textChanged {
			t.Errorf("page %s text differs\nwant:\n%s\ngot:\n%s", page, wantText, gotText)
		}

		ref := renderPage(t, want, page)
		diff := renderPage(t, got, page)
		if diff.Bounds() != ref.Bounds() {
			t.Fatalf("page %s raster bounds: got %v, want %v", page, diff.Bounds(), ref.Bounds())
		}
		bounds := ref.Bounds()
		var changed int64
		for y := 0; y < bounds.Dy(); y++ {
			for x := 0; x < bounds.Dx(); x++ {
				if diff.RGBAAt(x, y) != ref.RGBAAt(x, y) {
					changed++
					diff.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
				}
			}
		}
		total := int64(bounds.Dx()) * int64(bounds.Dy())
		changedPercent := 100 * float64(changed) / float64(total)
		t.Logf("page %s: %d/%d pixels differ (%.4f%%)", page, changed, total, changedPercent)
		overLimit := changedPercent >= visualTolerancePercent
		if overLimit {
			t.Errorf("page %s visual difference must be below %g%%", page, visualTolerancePercent)
		}
		if textChanged || overLimit {
			var output bytes.Buffer
			if err := png.Encode(&output, diff); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(t.ArtifactDir(), "diff-page-"+page+".png"), output.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func renderPage(t *testing.T, pdf []byte, page string) *image.RGBA {
	t.Helper()
	data := poppler(t, pdf, "pdftoppm", "-f", page, "-l", page, "-singlefile", "-r", "144", "-png", "-")
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba
}
