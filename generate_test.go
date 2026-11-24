package qfreportpdf_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/blutspende/qfreportpdf"
	"golang.org/x/image/font/gofont/goregular"
)

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

func TestGenerateBarcodeTypeAndCheckDigit(t *testing.T) {
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
	generate := func(kind, content string) ([]byte, error) {
		return qfreportpdf.Generate([]byte(fmt.Sprintf(template, kind, content)), []byte(`{"rows":[]}`))
	}

	for _, tc := range []struct{ kind, content string }{
		{"EAN13", "1234567"},
		{"EAN8", "400638133393"},
		{"UPC", "4006381333931"},
		{"EAN2", "12345"},
		{"EAN5", "12"},
		{"ITF-14", "1234"},
		{"ITF-14", "12345678901239"},
	} {
		t.Run(tc.kind+"_"+tc.content, func(t *testing.T) {
			pdf, err := generate(tc.kind, tc.content)
			if pdf != nil || err == nil || !strings.Contains(err.Error(), "encode "+tc.kind+" barcode:") {
				t.Fatalf("Generate returned %d bytes and error %v, want nil PDF and a barcode encoding error", len(pdf), err)
			}
		})
	}

	t.Run("ITF-14 calculates the check digit", func(t *testing.T) {
		want, err := generate("ITF-14", "12345678901231")
		if err != nil {
			t.Fatal(err)
		}
		got, err := generate("ITF-14", "1234567890123")
		if err != nil {
			t.Fatal(err)
		}
		comparePDF(t, want, got)
	})
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

	t.Run("embedded TrueType font", func(t *testing.T) {
		font := "data:font/ttf;base64," + base64.StdEncoding.EncodeToString(goregular.TTF)
		fonts := fmt.Sprintf(`{"Helvetica":{"url":%q}}`, font)
		pdf, err := generate(`{"textMode":"windows-1252"}`, fonts, name)
		if err != nil {
			t.Fatal(err)
		}
		text := poppler(t, pdf, "pdftotext", "-enc", "UTF-8", "-", "-")
		if got := strings.TrimSpace(string(text)); got != name {
			t.Fatalf("rendered name = %q, want %q", got, name)
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
