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

1. **Set up the page and fonts.** Set the page dimensions, units, orientation, and margins. This example uses A4 in millimeters with 15 mm margins. With the default text mode and an empty `fonts` object, the library uses its bundled font.
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

### Fit text in a box

To give long order IDs two lines at 11 pt, set the `orderRow` band’s `height` to `12` and replace its text element with this one.

```json
{
  "id": "order", "name": "Order ID and quantity", "type": "text",
  "rect": { "x": 0, "y": 0, "w": 55, "h": 10 },
  "style": { "font": "Helvetica", "size": 11 },
  "wordWrap": true,
  "autoSize": false,
  "text": "[orders.id] (quantity: [orders.quantity])"
}
```

A short ID fits on one line. A longer ID, such as `de36a28c-8676-4f5a-87fc-82df1935e971`, makes the text wrap inside the 55 mm box. With `autoSize: false`, the font stays at 11 pt. Set the box and band heights to leave enough room for the expected lines.

Set `autoSize` to `true` to let the renderer shrink the font until the text fits the box’s width and height. With `wordWrap: false`, this can keep an order ID on one line. The box dimensions stay fixed in either case. If the text still cannot fit at 4 pt, or at its configured size when that size is already smaller, `Generate` returns an error identifying the element.

### Add page numbers

Append this band to the template’s `bands` array to print a page number at the bottom right of every page.

```json
{
  "id": "pageNumbers", "kind": "pageFooter", "height": 8,
  "elements": [{
    "id": "pageNumber", "name": "Page number", "type": "text",
    "rect": { "x": 0, "y": 0, "w": 180, "h": 6 },
    "style": { "font": "Helvetica", "size": 10 },
    "horizontalAlign": "end",
    "text": "Page [PAGE()] of [PAGETOTAL()]"
  }]
}
```

The first page of a three-page report prints **Page 1 of 3**. The renderer resolves the numbers before fitting and wrapping the text.

### Choose fonts and character coverage

Template and data JSON use UTF-8. By default, `defaults.textMode` is `unicode`. With `"fonts": {}`, the renderer embeds the bundled Go TrueType font in the requested regular, bold, italic, or bold italic style. If the font lacks a glyph, `Generate` reports the band, element, and Unicode code point.

To use built-in PDF font metrics, add these defaults at the top level of the template.

```json
{
  "defaults": {
    "font": "Helvetica",
    "textMode": "windows-1252"
  }
}
```

This mode maps unembedded font names to built-in Helvetica, Times, or Courier. For example, `Café` renders with built-in Helvetica, while `東京` returns an error because those characters are outside Windows-1252. Changing text modes can change wrapping because the fonts have different metrics.

To embed Inter instead, replace the template’s `fonts` object with the one below, substitute the base64 of your TrueType file for `BASE64_TTF`, and set the desired text element’s `style.font` to `"Inter"`.

```json
{
  "fonts": {
    "Inter": { "url": "data:font/ttf;base64,BASE64_TTF" }
  }
}
```

An embedded font takes precedence in either text mode. See `$defs.fontName` in the [report schema](internal/render/report.schema.json) for the names accepted by `defaults.font`, `style.font`, and `fonts`.

### Embed an image

Add this element to the `heading` band’s `elements` array to place a blue circle in a 10 × 10 mm box near the right margin.

```json
{
  "id": "mark", "name": "Blue circle", "type": "image",
  "rect": { "x": 165, "y": 0, "w": 10, "h": 10 },
  "style": {},
  "image": {
    "mime": "image/svg+xml",
    "data": "PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxMCAxMCI+PGNpcmNsZSBjeD0iNSIgY3k9IjUiIHI9IjQiIGZpbGw9IiMyNTYzZWIiLz48L3N2Zz4="
  }
}
```

For your own image, put the base64-encoded file bytes in `image.data` and set `image.mime` to `image/png`, `image/jpeg`, or `image/svg+xml`. The renderer reads the asset from the template in memory.

### Add a numeric barcode

Add this element to the `heading` band’s `elements` array to print an EAN-13 barcode beside the heading.

```json
{
  "id": "productCode", "name": "Product barcode", "type": "barcode",
  "rect": { "x": 100, "y": 0, "w": 55, "h": 12 },
  "style": {},
  "barcodeType": "EAN13",
  "text": "012345678901"
}
```

This encodes `0123456789012`. The renderer calculates the final `2` from the 12 digits supplied. Supplying all 13 digits requires a valid check digit. Use strings for barcode values to preserve leading zeros.

| `barcodeType` | Digits without check digit | Digits with check digit |
| --- | ---: | ---: |
| `EAN13` | 12 | 13 |
| `EAN8` | 7 | 8 |
| `UPC` (UPC-A) | 11 | 12 |
| `ITF-14` | 13 | 14 |

`EAN2` and `EAN5` require exactly 2 and 5 digits, respectively. SVG images and barcodes are converted to pixels using the physical size of their boxes. Equivalent boxes in `mm`, `inch`, and `px` have the same raster density, with `px` based on 96 pixels per inch.

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
