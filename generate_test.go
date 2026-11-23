package qfreportpdf_test

import (
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
