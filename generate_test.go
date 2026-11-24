package qfreportpdf_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/blutspende/qfreportpdf"
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
