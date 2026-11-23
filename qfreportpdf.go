// Package qfreportpdf renders band-based report templates with caller-supplied data.
package qfreportpdf

import (
	"github.com/blutspende/qfreportpdf/internal/render"
)

// Generate validates the template and its data, then returns the rendered PDF.
// Both inputs are JSON documents. On error, it returns a nil byte slice.
func Generate(templateJSON, dataJSON []byte) ([]byte, error) {
	report, err := render.ParseTemplate(templateJSON)
	if err != nil {
		return nil, err
	}
	rows, err := render.ParseData(dataJSON, report.DataSources)
	if err != nil {
		return nil, err
	}
	return render.PDF(report, rows)
}
