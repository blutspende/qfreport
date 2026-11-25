package qfreportpdf_test

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image/png"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/blutspende/qfreportpdf"
	"golang.org/x/image/font/gofont/goregular"
)

func TestGenerateRejectsUnsupportedDataSourceDeclarations(t *testing.T) {
	const template = `{
		"page":{"size":"A4","orientation":"portrait","unit":"mm","width":210,"height":297,
			"margin":{"top":15,"right":15,"bottom":15,"left":15}},
		"fonts":{},
		"dataSources":{%q:{"kind":"table","columns":[{"name":%q,"type":"string"}]%s}},
		"bands":[{"id":"title","kind":"title","height":10,"elements":[]}]
	}`
	cases := []struct {
		name   string
		source string
		column string
		extra  string
		want   string
	}{
		{name: "source name with a space", source: "order list", column: "id", want: "invalid propertyName 'order list'"},
		{name: "column name with a space", source: "orders", column: "order id", want: "/dataSources/orders/columns/0/name"},
		{name: "memory source", source: "orders", column: "id", extra: `,"source":"memory"`, want: "additional properties 'source' not allowed"},
		{name: "CSV source", source: "orders", column: "id", extra: `,"source":"csv"`, want: "additional properties 'source' not allowed"},
		{name: "CSV filename", source: "orders", column: "id", extra: `,"csvFileName":"orders.csv"`, want: "csvFileName"},
		{name: "embed flag", source: "orders", column: "id", extra: `,"embed":true`, want: "additional properties 'embed' not allowed"},
		{name: "sample rows", source: "orders", column: "id", extra: `,"samples":[{"id":"ORD-001"}]`, want: "additional properties 'samples' not allowed"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			layout := []byte(fmt.Sprintf(template, tt.source, tt.column, tt.extra))
			data := []byte(fmt.Sprintf(`{%q:[]}`, tt.source))
			pdf, err := qfreportpdf.Generate(layout, data)
			if pdf != nil || err == nil || !strings.HasPrefix(err.Error(), "template:") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Generate returned %d bytes and error %v, want nil PDF and a template error about %q", len(pdf), err, tt.want)
			}
		})
	}
}

func TestGenerateGroupBandOrder(t *testing.T) {
	template := []byte(`{
		"page": {"size":"A4","orientation":"portrait","unit":"mm","width":210,"height":297,
			"margin":{"top":15,"right":15,"bottom":15,"left":15}},
		"fonts": {},
		"dataSources": {"orders":{"kind":"table","columns":[{"name":"region","type":"string"},{"name":"id","type":"string"}]}},
		"bands": [
			{"id":"header","kind":"groupHeader","height":8,"dataSource":"orders","groupBy":["region"],"elements":[
				{"id":"region","name":"Region","type":"text","rect":{"x":0,"y":0,"w":80,"h":7},"style":{},"text":"[orders.region]"}]},
			{"id":"detail","kind":"detail","height":8,"dataSource":"orders","elements":[
				{"id":"order","name":"Order","type":"text","rect":{"x":0,"y":0,"w":80,"h":7},"style":{},"text":"[orders.id]"}]},
			{"id":"footer","kind":"groupFooter","height":8,"dataSource":"orders","groupBy":["region"],"elements":[
				{"id":"end","name":"End","type":"text","rect":{"x":0,"y":0,"w":80,"h":7},"style":{},"text":"End [orders.region]"}]}
		]
	}`)
	data := []byte(`{"orders":[
		{"region":"North","id":"N-01"},
		{"region":"South","id":"S-01"},
		{"region":"North","id":"N-02"}
	]}`)
	pdf, err := qfreportpdf.Generate(template, data)
	if err != nil {
		t.Fatal(err)
	}

	text := poppler(t, pdf, "pdftotext", "-layout", "-", "-")
	var got []string
	for line := range strings.SplitSeq(string(text), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			got = append(got, trimmed)
		}
	}
	want := []string{"North", "N-01", "N-02", "End North", "South", "S-01", "End South"}
	if !slices.Equal(got, want) {
		t.Fatalf("grouped band order = %q, want %q", got, want)
	}
}

func TestGenerateGroupedResultComments(t *testing.T) {
	const template = `{
		"page":{"size":"custom","orientation":"landscape","unit":"mm","width":110,"height":65,
			"margin":{"top":5,"right":5,"bottom":5,"left":5}},
		"fonts":{},
		"dataSources":{"results":{"kind":"table","columns":[{"name":"sample","type":"string"},
			{"name":"code","type":"string"},{"name":"comment","type":"string"}]}},
		"bands":[
			{"id":"sample","kind":"groupHeader","height":10,"dataSource":"results","groupBy":["sample"],"elements":[
				{"id":"heading","name":"Sample","type":"text","rect":{"x":0,"y":0,"w":95,"h":8},"style":{},"text":"Sample [results.sample]"}]},
			{"id":"result","kind":"detail","height":10,"dataSource":"results","elements":[
				{"id":"code","name":"Result","type":"text","rect":{"x":0,"y":0,"w":95,"h":8},"style":{},"binding":"results.code"}]},
			{"id":"comment","kind":%q,"height":10,"dataSource":"results","elements":[
				{"id":"note","name":"Comment","type":"text","rect":{"x":0,"y":0,"w":95,"h":8},"style":{},"binding":"results.comment"}]}
		]
	}`
	data := []byte(`{"results":[
		{"sample":"EDTA","code":"HGB","comment":"Repeat confirmed"},
		{"sample":"EDTA","code":"PLT","comment":"Platelet clumps"},
		{"sample":"Serum","code":"K","comment":"Slight hemolysis"}
	]}`)
	// The EDTA heading and two result/comment pairs leave only 5 mm for the next 10 mm heading.
	want := []string{"Sample EDTA HGB Repeat confirmed PLT Platelet clumps", "Sample Serum K Slight hemolysis"}
	for _, kind := range []string{"child", "detail"} {
		t.Run(kind, func(t *testing.T) {
			pdf, err := qfreportpdf.Generate([]byte(fmt.Sprintf(template, kind)), data)
			if err != nil {
				t.Fatal(err)
			}
			text := poppler(t, pdf, "pdftotext", "-layout", "-", "-")
			pages := strings.Split(strings.TrimSuffix(string(text), "\f"), "\f")
			for i, page := range pages {
				pages[i] = strings.Join(strings.Fields(page), " ")
			}
			if !slices.Equal(pages, want) {
				t.Fatalf("results and comments by page = %q, want %q", pages, want)
			}
		})
	}
}

func TestGenerateChildBands(t *testing.T) {
	const template = `{
		"page":{"size":"custom","orientation":"landscape","unit":"mm","width":110,"height":70,
			"margin":{"top":5,"right":5,"bottom":5,"left":5}},
		"fonts":{},
		"dataSources":{"results":{"kind":"table","columns":[{"name":"sample","type":"string"},
			{"name":"code","type":"string"},{"name":"comment","type":"string"}]}},
		"bands":[%s]
	}`
	band := func(id, kind, extra, text string) string {
		return fmt.Sprintf(`{"id":%q,"kind":%q,"height":5%s,"elements":[
			{"id":"text","name":"Text","type":"text","rect":{"x":0,"y":0,"w":95,"h":5},"style":{},"text":%q}]}`, id, kind, extra, text)
	}
	group := `,"dataSource":"results","groupBy":["sample"]`
	data := []byte(`{"results":[
		{"sample":"EDTA","code":"HGB","comment":"Repeat confirmed"},
		{"sample":"EDTA","code":"PLT","comment":"Platelet clumps"},
		{"sample":"Serum","code":"K","comment":"Slight hemolysis"}
	]}`)

	t.Run("each parent kind", func(t *testing.T) {
		bands := strings.Join([]string{
			band("page", "pageHeader", "", "Lab report"),
			band("patient", "child", "", "Patient 42"),
			band("sample", "groupHeader", group, "Sample [results.sample]"),
			band("columns", "child", "", "Code / comment"),
			band("result", "detail", `,"dataSource":"results"`, "[results.code]"),
			band("comment", "child", "", "[results.comment]"),
			band("end", "groupFooter", group, "End [results.sample]"),
			band("signed", "child", "", "Validated [results.sample]"),
			band("footer", "pageFooter", "", "Page [PAGE()]"),
			band("legend", "child", "", "Confidential"),
		}, ",")
		pdf, err := qfreportpdf.Generate([]byte(fmt.Sprintf(template, bands)), data)
		if err != nil {
			t.Fatal(err)
		}
		text := poppler(t, pdf, "pdftotext", "-layout", "-", "-")
		pages := strings.Split(strings.TrimSuffix(string(text), "\f"), "\f")
		for i, page := range pages {
			pages[i] = strings.Join(strings.Fields(page), " ")
		}
		// Header and footer children leave 40 mm, so each sample's eight 5 mm bands fill one page.
		want := []string{
			"Lab report Patient 42 Sample EDTA Code / comment HGB Repeat confirmed PLT Platelet clumps End EDTA Validated EDTA Page 1 Confidential",
			"Lab report Patient 42 Sample Serum Code / comment K Slight hemolysis End Serum Validated Serum Page 2 Confidential",
		}
		if !slices.Equal(pages, want) {
			t.Fatalf("pages = %q, want %q", pages, want)
		}
	})

	for _, tc := range []struct{ name, bands, want string }{
		{"first band", band("note", "child", "", "Note"), "template.bands[0].kind: a child band must follow another band"},
		{"other source", band("page", "pageHeader", "", "Lab report") + "," + band("note", "child", `,"dataSource":"results"`, "[results.code]"),
			`template.bands[1].dataSource: source "results" differs from band "page"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf, err := qfreportpdf.Generate([]byte(fmt.Sprintf(template, tc.bands)), data)
			if pdf != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Generate returned %d bytes and error %v, want nil PDF and %q", len(pdf), err, tc.want)
			}
		})
	}
}

func TestGenerateTypedCells(t *testing.T) {
	template := []byte(`{
		"page":{"size":"A4","orientation":"portrait","unit":"mm","width":210,"height":297,
			"margin":{"top":10,"right":10,"bottom":10,"left":10}},
		"fonts":{},
		"dataSources":{"rows":{"kind":"table","columns":[{"name":"id","type":"integer"},
			{"name":"reading","type":"decimal"},{"name":"flag","type":"boolean"}]}},
		"bands":[{"id":"row","kind":"detail","height":9,"dataSource":"rows","elements":[
			{"id":"cells","name":"Cells","type":"text","rect":{"x":0,"y":0,"w":180,"h":8},"style":{},
				"text":"ID=([rows.id]) reading=([rows.reading]) flag=([rows.flag])"}]}]
	}`)
	for _, tc := range []struct{ name, data, want string }{
		{"values and blanks", `{"rows":[{"id":9007199254740993,"reading":1.2300,"flag":false},{"id":0,"reading":null}]}`,
			"ID=(9007199254740993) reading=(1.2300) flag=(false) ID=(0) reading=() flag=()"},
		{"empty table", `{"rows":[]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf, err := qfreportpdf.Generate(template, []byte(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			text := poppler(t, pdf, "pdftotext", "-layout", "-", "-")
			if got := strings.Join(strings.Fields(string(text)), " "); got != tc.want {
				t.Fatalf("printed cells = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGeneratePaginationReservesHeaderAndFooterSpace(t *testing.T) {
	const template = `{
		"page":{"size":"custom","orientation":"portrait","unit":"mm","width":100,"height":140.7,
			"margin":{"top":10.3,"right":10,"bottom":10.3,"left":10}},
		"fonts":{},
		"dataSources":{"rows":{"kind":"table","columns":[{"name":"value","type":"string"}]}},
		"bands":[
			{"id":"page-header","kind":"pageHeader","height":18.1,"elements":[]},
			{"id":"column-header","kind":"columnHeader","height":12,"elements":[]},
			{"id":"detail","kind":"detail","height":%d,"dataSource":"rows","elements":[
				{"id":"value","name":"Value","type":"text","rect":{"x":0,"y":0,"w":80,"h":7},"style":{},"text":"[rows.value]"}]},
			{"id":"column-footer","kind":"columnFooter","height":12,"elements":[]},
			{"id":"page-footer","kind":"pageFooter","height":8,"elements":[]}
		]
	}`
	data := []byte(`{"rows":[{"value":"first"},{"value":"second"}]}`)

	t.Run("oversized detail", func(t *testing.T) {
		pdf, err := qfreportpdf.Generate([]byte(fmt.Sprintf(template, 80)), data)
		if pdf != nil || err == nil || !strings.Contains(err.Error(), `band "detail" is taller than the printable page area`) {
			t.Fatalf("Generate returned %d bytes and error %v, want nil PDF and an oversized band error", len(pdf), err)
		}
	})

	t.Run("exact fit on each page", func(t *testing.T) {
		pdf, err := qfreportpdf.Generate([]byte(fmt.Sprintf(template, 70)), data)
		if err != nil {
			t.Fatal(err)
		}
		text := poppler(t, pdf, "pdftotext", "-layout", "-", "-")
		pages := strings.Split(strings.TrimSuffix(string(text), "\f"), "\f")
		for i := range pages {
			pages[i] = strings.TrimSpace(pages[i])
		}
		want := []string{"first", "second"}
		if !slices.Equal(pages, want) {
			t.Fatalf("pages = %q, want %q", pages, want)
		}
	})
}

func generateBarcode(kind, content string) ([]byte, error) {
	const template = `{
		"page":{"size":"A4","orientation":"portrait","unit":"mm","width":210,"height":297,
			"margin":{"top":15,"right":15,"bottom":15,"left":15}},
		"fonts":{},
		"dataSources":{"rows":{"kind":"table","columns":[{"name":"value","type":"string"}]}},
		"bands":[{"id":"barcodes","kind":"title","height":20,"elements":[
			{"id":"code","name":"Code","type":"barcode","rect":{"x":0,"y":0,"w":48,"h":12},
				"style":{},"barcodeType":%q,"text":%q}
		]}]
	}`
	return qfreportpdf.Generate([]byte(fmt.Sprintf(template, kind, content)), []byte(`{"rows":[]}`))
}

func TestGenerateBarcodeTypeAndCheckDigit(t *testing.T) {
	for _, tc := range []struct{ kind, content string }{
		{"EAN13", "1234567"},
		{"EAN8", "400638133393"},
		{"UPC", "4006381333931"},
		{"EAN2", "12345"},
		{"EAN5", "12"},
		{"ITF-14", "1234"},
		{"ITF-14", "12345678901239"},
		{"MSI", "+123"},
	} {
		t.Run(tc.kind+"_"+tc.content, func(t *testing.T) {
			pdf, err := generateBarcode(tc.kind, tc.content)
			if pdf != nil || err == nil || !strings.Contains(err.Error(), "encode "+tc.kind+" barcode:") {
				t.Fatalf("Generate returned %d bytes and error %v, want nil PDF and a barcode encoding error", len(pdf), err)
			}
		})
	}

	t.Run("ITF-14 calculates the check digit", func(t *testing.T) {
		want, err := generateBarcode("ITF-14", "12345678901231")
		if err != nil {
			t.Fatal(err)
		}
		got, err := generateBarcode("ITF-14", "1234567890123")
		if err != nil {
			t.Fatal(err)
		}
		comparePDF(t, want, got)
	})

	t.Run("20-digit MSI", func(t *testing.T) {
		pdf, err := generateBarcode("MSI", "12345678901234567890")
		if err != nil || len(pdf) == 0 {
			t.Fatalf("Generate returned %d bytes and %v for a 20-digit MSI value", len(pdf), err)
		}
	})
}

func TestGenerateBarcodeSymbols(t *testing.T) {
	t.Run("EAN-8 decoded digits", func(t *testing.T) {
		pdf, err := generateBarcode("EAN8", "2000032")
		if err != nil {
			t.Fatal(err)
		}
		widths := barcodeWidths(t, pdf)
		// GS1 General Specifications §5.2. Each half has four digits of four runs each.
		if len(widths) != 43 || widths[:3] != "111" || widths[19:24] != "11111" || widths[40:] != "111" {
			t.Fatalf("invalid EAN-8 length or guard bars in %q", widths)
		}
		patterns := []string{"3211", "2221", "2122", "1411", "1132", "1231", "1114", "1312", "1213", "3112"}
		var digits strings.Builder
		data := widths[3:19] + widths[24:40]
		for i := 0; i < len(data); i += 4 {
			pattern := data[i : i+4]
			digit := slices.Index(patterns, pattern)
			if digit < 0 {
				t.Fatalf("unknown EAN-8 digit pattern %q", pattern)
			}
			digits.WriteByte('0' + byte(digit))
		}
		if got := digits.String(); got != "20000325" {
			t.Fatalf("decoded EAN-8 = %q, want 20000325 including the check digit", got)
		}
	})

}

// barcodeWidths reads alternating bars and spaces in modules from the single barcode in a PDF.
func barcodeWidths(t *testing.T, pdf []byte) string {
	t.Helper()
	prefix := filepath.Join(t.TempDir(), "barcode")
	poppler(t, pdf, "pdfimages", "-png", "-", prefix)
	img, err := png.Decode(bytes.NewReader(readFixture(t, prefix+"-000.png")))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	y := bounds.Min.Y + bounds.Dy()/2
	var runs []int
	var lastBlack bool
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		r, _, _, _ := img.At(x, y).RGBA()
		black := r < 0x8000
		if black != lastBlack {
			runs = append(runs, 0)
			lastBlack = black
		}
		if len(runs) > 0 {
			runs[len(runs)-1]++
		}
	}
	if len(runs) == 0 {
		t.Fatal("barcode image has no bars")
	}
	if !lastBlack {
		runs = runs[:len(runs)-1]
	}
	unit := slices.Min(runs)
	var widths strings.Builder
	for _, run := range runs {
		if run%unit != 0 || run/unit > 4 {
			t.Fatalf("unexpected bar or space width %d for a %d-pixel module", run, unit)
		}
		widths.WriteByte('0' + byte(run/unit))
	}
	return widths.String()
}

func TestGenerateUnicodeText(t *testing.T) {
	const template = `{
		"page":{"size":"A4","orientation":"portrait","unit":"mm","width":210,"height":297,
			"margin":{"top":15,"right":15,"bottom":15,"left":15}},
		"defaults":%s,
		"fonts":%s,
		"dataSources":{"people":{"kind":"table","columns":[{"name":"name","type":"string"}]}},
		"bands":[{"id":"title","kind":"title","height":20,"elements":[
			{"id":"name","name":"Name","type":"text","rect":{"x":0,"y":0,"w":80,"h":12},
				"style":{"font":"Helvetica","bold":true},"text":"[people.name]"}
		]}]
	}`
	const name = "Őri Gyűjtő"
	generate := func(defaults, fonts, text string) ([]byte, error) {
		data := []byte(fmt.Sprintf(`{"people":[{"name":%q}]}`, text))
		return qfreportpdf.Generate([]byte(fmt.Sprintf(template, defaults, fonts)), data)
	}

	for _, tc := range []struct{ name, defaults, fontType string }{
		{"omitted mode", `{}`, "CID TrueType"},
		{"unicode mode", `{"textMode":"unicode"}`, "CID TrueType"},
		{"windows-1252 mode", `{"textMode":"windows-1252"}`, "WinAnsi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf, err := generate(tc.defaults, "{}", "Plain text")
			if err != nil {
				t.Fatal(err)
			}
			fonts := poppler(t, pdf, "pdffonts", "-")
			if !strings.Contains(string(fonts), tc.fontType) {
				t.Fatalf("fonts = %s, want %s", fonts, tc.fontType)
			}
		})
	}

	t.Run("unicode name", func(t *testing.T) {
		pdf, err := generate("{}", "{}", name)
		if err != nil {
			t.Fatal(err)
		}
		text := poppler(t, pdf, "pdftotext", "-enc", "UTF-8", "-", "-")
		if got := strings.TrimSpace(string(text)); got != name {
			t.Fatalf("rendered name = %q, want %q", got, name)
		}
	})

	for _, tc := range []struct{ name, defaults, text, wantError string }{
		{"missing glyph", `{}`, "Őri 中", `render band "title" element "name": bundled Go font cannot render U+4E2D`},
		{"outside windows-1252", `{"textMode":"windows-1252"}`, name, `built-in font "Helvetica" cannot render U+0150`},
		{"invalid mode", `{"textMode":"utf-8"}`, name, "/defaults/textMode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf, err := generate(tc.defaults, "{}", tc.text)
			if pdf != nil || err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Generate returned %d bytes and error %v, want nil PDF and %q", len(pdf), err, tc.wantError)
			}
		})
	}

	font := "data:font/ttf;base64," + base64.StdEncoding.EncodeToString(goregular.TTF)
	fonts := fmt.Sprintf(`{"Helvetica":{"url":%q}}`, font)
	t.Run("embedded TrueType font", func(t *testing.T) {
		pdf, err := generate(`{"textMode":"windows-1252"}`, fonts, name)
		if err != nil {
			t.Fatal(err)
		}
		text := poppler(t, pdf, "pdftotext", "-enc", "UTF-8", "-", "-")
		if got := strings.TrimSpace(string(text)); got != name {
			t.Fatalf("rendered name = %q, want %q", got, name)
		}
	})

	t.Run("embedded font missing check mark", func(t *testing.T) {
		pdf, err := generate("{}", fonts, "Verified ✓")
		if pdf != nil || err == nil || !strings.Contains(err.Error(), `band "title" element "name"`) || !strings.Contains(err.Error(), "U+2713") {
			t.Fatalf("Generate returned %d bytes and %v, want nil PDF and an error identifying the field and missing U+2713", len(pdf), err)
		}
	})

	t.Run("page total glyph", func(t *testing.T) {
		render := func(text string) []byte {
			t.Helper()
			layout := strings.Replace(fmt.Sprintf(template, "{}", "{}"), "[people.name]", text, 1)
			pdf, err := qfreportpdf.Generate([]byte(layout), []byte(`{"people":[]}`))
			if err != nil {
				t.Fatal(err)
			}
			return renderPage(t, pdf, "1").Pix
		}
		if !bytes.Equal(render("Total [PAGETOTAL()]"), render("Total 1")) {
			t.Fatal("the rendered page total should match the literal digit")
		}
	})
}

func TestGenerateRasterSizeAcrossPageUnits(t *testing.T) {
	const template = `{
		"page":{"size":"custom","orientation":"landscape","unit":%q,"width":%.6f,"height":%.6f,
			"margin":{"top":0,"right":0,"bottom":0,"left":0}},
		"fonts":{},
		"dataSources":{"rows":{"kind":"table","columns":[{"name":"value","type":"string"}]}},
		"bands":[{"id":"assets","kind":"title","height":%.6f,"elements":[
			{"id":"svg","name":"SVG","type":"image","rect":{"x":0,"y":0,"w":%.6f,"h":%.6f},
				"style":{},"image":{"mime":"image/svg+xml","data":%q}},
			{"id":"ean","name":"EAN","type":"barcode","rect":{"x":%.6f,"y":0,"w":%.6f,"h":%.6f},
				"style":{},"barcodeType":"EAN13","text":"4006381333931"}
		]}]
	}`
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 12"><rect width="40" height="12" fill="#334455"/></svg>`
	imageData := base64.StdEncoding.EncodeToString([]byte(svg))
	for _, tc := range []struct {
		unit  string
		scale float64
	}{
		{"mm", 1},
		{"inch", 1.0 / 25.4},
		{"px", 96.0 / 25.4},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			s := tc.scale
			layout := fmt.Sprintf(template, tc.unit, 152.4*s, 101.6*s, 30*s, 40*s, 12*s, imageData, 45*s, 40*s, 12*s)
			pdf, err := qfreportpdf.Generate([]byte(layout), []byte(`{"rows":[]}`))
			if err != nil {
				t.Fatal(err)
			}
			list := poppler(t, pdf, "pdfimages", "-list", "-")
			var sizes [][2]string
			for line := range strings.SplitSeq(string(list), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 5 && fields[2] == "image" {
					sizes = append(sizes, [2]string{fields[3], fields[4]})
				}
			}
			want := [][2]string{{"160", "48"}, {"320", "96"}}
			if !slices.Equal(sizes, want) {
				t.Fatalf("raster dimensions = %v, want %v", sizes, want)
			}
		})
	}
}

func TestGenerateTextFitting(t *testing.T) {
	template := []byte(`{
		"page":{"size":"custom","orientation":"landscape","unit":"mm","width":100,"height":70,
			"margin":{"top":10,"right":10,"bottom":10,"left":10}},
		"fonts":{},
		"dataSources":{"rows":{"kind":"table","columns":[{"name":"label","type":"string"}]}},
		"bands":[
			{"id":"detail","kind":"detail","height":30,"dataSource":"rows","elements":[
				{"id":"label","name":"Label","type":"text","rect":{"x":0,"y":0,"w":22,"h":8},
					"autoSize":true,"wordWrap":false,"style":{"size":14},"text":"[rows.label]"}]},
			{"id":"footer","kind":"pageFooter","height":20,"elements":[
				{"id":"total","name":"Total pages","type":"text","rect":{"x":0,"y":0,"w":4,"h":19},
					"wordWrap":true,"style":{"size":10},"binding":"[PAGETOTAL()]"}]}
		]
	}`)
	data := []byte(`{"rows":[{"label":"A long order reference"},{"label":"Another long reference"}]}`)
	pdf, err := qfreportpdf.Generate(template, data)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("autoSize fits box width", func(t *testing.T) {
		var doc struct {
			Words []struct {
				Text string  `xml:",chardata"`
				XMax float64 `xml:"xMax,attr"`
			} `xml:"body>doc>page>word"`
		}
		if err := xml.Unmarshal(poppler(t, pdf, "pdftotext", "-bbox", "-", "-"), &doc); err != nil {
			t.Fatal(err)
		}
		const rightEdge = (10.0 + 22.0) * 72 / 25.4
		for _, word := range doc.Words {
			if word.XMax > rightEdge {
				t.Errorf("word %q ends at %.2f pt, past the box edge at %.2f pt", word.Text, word.XMax, rightEdge)
			}
		}
	})

	t.Run("wrapped final page total", func(t *testing.T) {
		text := poppler(t, pdf, "pdftotext", "-layout", "-", "-")
		pages := strings.Split(strings.TrimSuffix(string(text), "\f"), "\f")
		for i, page := range pages {
			pages[i] = strings.Join(strings.Fields(page), " ")
		}
		want := []string{"A long order reference 2", "Another long reference 2"}
		if !slices.Equal(pages, want) {
			t.Fatalf("pages = %q, want %q", pages, want)
		}
	})

	t.Run("text cannot fit at minimum size", func(t *testing.T) {
		narrow := bytes.Replace(template, []byte(`"w":22`), []byte(`"w":2`), 1)
		pdf, err := qfreportpdf.Generate(narrow, data)
		if pdf != nil || err == nil || !strings.Contains(err.Error(), `render band "detail" element "label": text does not fit its box at 4 pt`) {
			t.Fatalf("Generate returned %d bytes and %v, want a contextual text fitting error", len(pdf), err)
		}
	})
}
