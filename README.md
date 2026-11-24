# qfreportpdf

Generate one PDF from two JSON documents. `templateJSON` defines the page layout and the fields the report expects. `dataJSON` supplies the values for those fields. One successful call to `Generate(templateJSON, dataJSON)` returns one complete PDF as a `[]byte`. A PDF can contain multiple pages.

## Install

The minimum supported Go version is **1.26.6**.

```sh
go get github.com/blutspende/qfreportpdf
```

## Regression testing

### Prerequisite: install Poppler

The tests in `generate_test.go` and `golden_test.go` use Poppler to inspect generated PDFs.

| Platform | Install command |
| --- | --- |
| macOS (Homebrew) | `brew install poppler` |
| Linux (Debian or Ubuntu) | `sudo apt update && sudo apt install poppler-utils` |
| Linux (Fedora) | `sudo dnf install poppler-utils` |
| Windows (PowerShell with WinGet) | `winget install --id oschwartz10612.Poppler --exact` |

After installation, open a new terminal and check that both commands print a version.

```sh
pdftotext -v
pdftoppm -v
```

### Run the golden tests

The tests use the **golden file testing pattern**. Each case has an approved PDF called `golden.pdf`. The test passes the suite’s template and the case’s data to `Generate`, then compares the result with that approved PDF.

```text
testdata/
└── labreport/
    ├── template.json
    ├── donor/
    │   ├── data.json
    │   └── golden.pdf
    └── patient/
        ├── data.json
        └── golden.pdf
```

The root [golden_test.go](golden_test.go) discovers every suite and case by folder name. Each suite has one `template.json` and at least one case folder. Each case contains exactly `data.json` and `golden.pdf`. To cover another input for the same template, add a sibling case folder. To cover a different template, add a suite under `testdata/`. Review a new or intentionally changed golden PDF before committing it.

From the module root, run all golden tests or select one by its folder path.

```sh
go test -count=1 -v -run '^TestGolden$' .
go test -count=1 -v -run '^TestGolden$/^labreport$/^patient$' .
```

Page count, page dimensions in points, and extracted text on each page must match exactly. Poppler renders both PDFs at 144 DPI. Each page must have fewer than 0.01% changed pixels. A pixel counts as changed if any color channel differs.

## 1. Build the layout in `templateJSON`

Suppose you want a list of orders with a heading and one line per order. Build the template in three steps:

1. **Set up the page and fonts.** Set the page dimensions, units, orientation, and margins. This example uses A4 in millimeters with 15 mm margins. An empty `fonts` object uses the default Unicode text mode.
2. **Declare the data model.** In `dataSources`, declare a data source called `orders` with a string column called `id` and an integer column called `quantity`. These declarations tell the library what to accept in `dataJSON`.
3. **Place the content in bands.** The `title` band prints once. The `detail` band repeats for each row in `orders`. Each element’s `rect` gives its position within the band and its dimensions, in the page’s units. Within text, `[orders.id]` and `[orders.quantity]` insert values from the current row.

Save this complete example as `template.json`:

```json
{
  "page": {
    "size": "A4", "orientation": "portrait", "unit": "mm",
    "width": 210, "height": 297,
    "margin": { "top": 15, "right": 15, "bottom": 15, "left": 15 }
  },
  "fonts": {},
  "dataSources": {
    "orders": {
      "kind": "table",
      "columns": [
        { "name": "id", "type": "string" },
        { "name": "quantity", "type": "integer" }
      ]
    }
  },
  "bands": [
    {
      "id": "heading", "kind": "title", "height": 14,
      "elements": [{
        "id": "title", "name": "Heading", "type": "text",
        "rect": { "x": 0, "y": 0, "w": 150, "h": 10 },
        "style": { "font": "Helvetica", "size": 16, "bold": true },
        "text": "Order summary"
      }]
    },
    {
      "id": "orderRow", "kind": "detail", "dataSource": "orders", "height": 8,
      "elements": [{
        "id": "order", "name": "Order ID and quantity", "type": "text",
        "rect": { "x": 0, "y": 0, "w": 150, "h": 7 },
        "style": { "font": "Helvetica", "size": 11 },
        "text": "[orders.id] (quantity: [orders.quantity])"
      }]
    }
  ]
}
```

The library validates `templateJSON` against the [embedded report schema](internal/render/report.schema.json), decodes it into its internal Go report model, and checks that bindings refer to declared sources and columns.

To group detail rows, place the detail bands between `groupHeader` and `groupFooter` bands that use the same data source and the same `groupBy` columns or `groupExpr`. Either group band is optional. Groups print in the order their first row appears. Within each group, rows retain their input order, and the first row supplies the bindings for the group header and footer.

Each detail or report band must fit between the page and column headers and footers on a fresh page. If a band is too tall for that space, `Generate` returns an error naming the band.

## 2. Supply runtime data in `dataJSON`

Save this as `data.json`. The key `orders` matches the data source in the template. Each object is one order to print.

```json
{
  "orders": [
    { "id": "ORD-001", "quantity": 2 },
    { "id": "ORD-002", "quantity": 5 }
  ]
}
```

The PDF will contain the heading **Order summary**, followed by **ORD-001 (quantity: 2)** and **ORD-002 (quantity: 5)**. To print a different set of orders, reuse the template and change the rows.

- **Match the model.** Supply one array for every source declared by the template. If your application uses Go structs for orders, map their fields to the declared columns before calling `Generate`. An unknown source or column returns an error.
- **Use the declared types.** For `quantity`, pass the JSON number `2`. The string `"2"` produces an error identifying the cell, such as `data.orders[0].quantity`. Decimal columns also take JSON numbers and preserve their precision. Boolean columns take `true` or `false`. String and expression columns take strings, date columns take dates such as `"2026-11-23"`, and `uuidv4` columns take hyphenated UUIDv4 strings.
- **Handle empty values.** An omitted or `null` cell prints as blank. Passing `{"orders": []}` prints the heading with zero order rows. All runtime rows come from `dataJSON`.

## 3. Generate the PDF in Go

Save this program as `main.go` alongside the two JSON files and run `go run .`. The program writes the returned PDF bytes to `orders.pdf`.

```go
package main

import (
	"log"
	"os"

	"github.com/blutspende/qfreportpdf"
)

func main() {
	templateJSON, err := os.ReadFile("template.json")
	if err != nil {
		log.Fatal(err)
	}
	dataJSON, err := os.ReadFile("data.json")
	if err != nil {
		log.Fatal(err)
	}

	pdf, err := qfreportpdf.Generate(templateJSON, dataJSON)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("orders.pdf", pdf, 0644); err != nil {
		log.Fatal(err)
	}
}
```

The caller chooses where the bytes go. For example, an HTTP handler can return them as `application/pdf`. On validation or rendering failure, `Generate` returns a nil slice and an error.

## Fonts and images

Only font names predefined in `$defs.fontName` in the [report schema](internal/render/report.schema.json) are accepted. This applies to `defaults.font`, each element’s `style.font`, and the keys in `fonts`. Examples include `Helvetica`, `Courier New`, and `Inter`.

Set `defaults.textMode` to choose how the PDF draws text when the selected font has no embedded font file. Both the template and report data are UTF-8 JSON.

| Mode | Font and character coverage |
| --- | --- |
| `unicode` (default) | Embeds the bundled Go TrueType font in the requested regular, bold, italic, or bold italic style. Use `"fonts": {}` to use the bundled font. If it lacks a glyph, `Generate` returns an error with the band, element, and Unicode code point. |
| `windows-1252` | Maps the selected font name to built-in Helvetica, Times, or Courier. Use this mode to preserve built-in font metrics. Characters outside Windows-1252 return an error. |

To choose built-in fonts, add this to the template.

```json
"defaults": {
  "textMode": "windows-1252"
}
```

An explicitly embedded TrueType font takes precedence in either mode. To use Inter, set `style.font` to `"Inter"` and put the base64-encoded TrueType file in `fonts.Inter.url` with the prefix `data:font/ttf;base64,`.

Bundled Go fonts have different metrics from the built-in PDF fonts. Changing modes can change wrapping and layout.

For an image element, set `image.mime` to `image/png`, `image/jpeg`, or `image/svg+xml`, and put the base64-encoded image bytes in `image.data`. Embed assets in the template to render entirely in memory.

## Numeric barcodes

Pass barcode values as strings to preserve leading zeros. `EAN13` accepts 12 or 13 digits, `EAN8` accepts 7 or 8, `UPC` (UPC-A) accepts 11 or 12, and `ITF-14` accepts 13 or 14. The shorter form calculates the check digit. The longer form must include a valid one. `EAN2` and `EAN5` require exactly 2 and 5 digits, respectively.
