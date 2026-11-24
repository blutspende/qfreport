package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/codabar"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/code39"
	"github.com/boombuler/barcode/ean"
	"github.com/boombuler/barcode/twooffive"
	"github.com/go-pdf/fpdf"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/text/encoding/charmap"
)

// Columns may be named with dotted paths. Each is a literal key in the row.
const bindingPath = `(\$root|[A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*(?:\.[A-Za-z_][A-Za-z0-9_-]*)*)`

var bindingPattern = regexp.MustCompile(`\[` + bindingPath + `\]`)
var bareBindingPattern = regexp.MustCompile(`^` + bindingPath + `$`)
var pageAliasPattern = regexp.MustCompile(`\{(nb|[0-9]+)\}`)

// Allow for floating-point rounding when a band fits exactly.
const pageFitTolerance = 1e-9

const bundledFontFamily = "go-unicode"

// renderContext holds one current row per data source. While drawing an order,
// ctx["orders"]["quantity"] reads the quantity from that order.
type renderContext map[string]map[string]string

type renderer struct {
	report        Report
	pdf           *fpdf.Fpdf
	scale         float64
	textMode      TextMode
	contentTop    float64
	contentY      float64
	contentBottom float64
	customFonts   map[FontName]bool
	bundledFonts  map[string]*sfnt.Font
	data          map[string][]map[string]string // All rows per source in input order, with display text keyed by column.
	pageAlias     string
	resourceID    int
}

// PDF renders the validated template with the supplied data already bound.
func PDF(report Report, data map[string][]map[string]string) (document []byte, err error) {
	// Some PDF, SVG, and font parsers panic on malformed assets, including
	// during final PDF serialization when the font subsets are generated.
	defer func() {
		if failure := recover(); failure != nil {
			document = nil
			err = fmt.Errorf("render PDF: %v", failure)
		}
	}()
	r, err := newRenderer(report, data)
	if err != nil {
		return nil, err
	}
	if err := r.render(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := r.pdf.Output(&output); err != nil {
		return nil, fmt.Errorf("write PDF: %w", err)
	}
	return output.Bytes(), nil
}

func newRenderer(report Report, data map[string][]map[string]string) (*renderer, error) {
	scale := 1.0
	unit := "mm"
	switch report.Page.Unit {
	case UnitInch:
		unit = "in"
	case UnitPixel:
		unit = "pt"
		scale = 72.0 / 96.0
	}

	width, height := report.Page.Width*scale, report.Page.Height*scale
	if report.Page.Orientation == OrientationLandscape && width < height {
		width, height = height, width
	}
	if report.Page.Orientation == OrientationPortrait && width > height {
		width, height = height, width
	}

	pdf := fpdf.NewCustom(&fpdf.InitType{
		OrientationStr: "P",
		UnitStr:        unit,
		Size:           fpdf.SizeType{Wd: width, Ht: height},
	})
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetMargins(0, 0, 0)
	pageAlias := selectPageAlias(report, data)
	pdf.AliasNbPages(pageAlias)
	pdf.SetCreator("qfreportpdf", true)
	if title, ok := report.Meta["title"].(string); ok {
		pdf.SetTitle(title, true)
	}

	r := &renderer{
		report:      report,
		pdf:         pdf,
		scale:       scale,
		textMode:    TextModeUnicode,
		customFonts: make(map[FontName]bool),
		data:        data,
		pageAlias:   pageAlias,
	}
	if defaults := report.Defaults; defaults != nil && defaults.TextMode != "" {
		r.textMode = defaults.TextMode
	}
	if err := r.loadFonts(); err != nil {
		return nil, err
	}
	r.contentTop = r.u(report.Page.Margin.Top)
	r.contentBottom = height - r.u(report.Page.Margin.Bottom)
	for _, band := range report.Bands {
		switch band.Kind {
		case BandPageHeader, BandColumnHeader:
			r.contentTop += r.u(band.Height)
		case BandPageFooter, BandColumnFooter:
			r.contentBottom -= r.u(band.Height)
		}
	}
	if r.contentTop > r.contentBottom+pageFitTolerance {
		return nil, fmt.Errorf("page margins, headers, and footers exceed the page height")
	}
	return r, nil
}

// fpdf substitutes its alias throughout the final page streams. Pick a short
// token absent from the text so a literal "{nb}" survives serialization.
func selectPageAlias(report Report, data map[string][]map[string]string) string {
	used := make(map[string]bool)
	record := func(value string) {
		if strings.Contains(value, "{") {
			for _, alias := range pageAliasPattern.FindAllString(value, -1) {
				used[alias] = true
			}
		}
	}
	for _, band := range report.Bands {
		for _, element := range band.Elements {
			record(element.Text)
			record(element.Expression)
		}
	}
	for _, rows := range data {
		for _, row := range rows {
			for _, value := range row {
				record(value)
			}
		}
	}
	alias := "{nb}"
	for i := 0; used[alias]; i++ {
		alias = fmt.Sprintf("{%02d}", i)
	}
	return alias
}

func (r *renderer) render() error {
	ctx := r.baseContext()
	if err := r.addPage(ctx); err != nil {
		return err
	}

	bands := make([]Band, 0, len(r.report.Bands))
	for _, band := range r.report.Bands {
		switch band.Kind {
		case BandPageHeader, BandColumnHeader, BandPageFooter, BandColumnFooter:
			continue
		}
		bands = append(bands, band)
	}

	for len(bands) > 0 {
		count, source, grouping := dataBandSection(bands)
		section := bands[:count]
		bands = bands[count:]
		if source == "" {
			if err := r.renderFlowBand(section[0], ctx); err != nil {
				return err
			}
			continue
		}
		groups := [][]map[string]string{r.data[source]}
		if grouping != nil {
			groups = r.groupRows(*grouping, source, ctx)
		}
		for _, rows := range groups {
			if err := r.renderDataBands(section, source, rows, ctx); err != nil {
				return err
			}
		}
	}
	if err := r.pdf.Error(); err != nil {
		return fmt.Errorf("render PDF: %w", err)
	}
	return nil
}

func (r *renderer) renderDataBands(bands []Band, source string, rows []map[string]string, base renderContext) error {
	if len(rows) == 0 {
		return nil
	}
	ctx := cloneContext(base)
	for _, band := range bands {
		bandRows := rows
		if band.Kind != BandDetail {
			bandRows = rows[:1]
		}
		for _, row := range bandRows {
			ctx[source] = row
			if err := r.renderFlowBand(band, ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *renderer) addPage(ctx renderContext) error {
	r.pdf.AddPage()
	r.contentY = r.contentTop

	headerY := r.u(r.report.Page.Margin.Top)
	for _, band := range r.report.Bands {
		if band.Kind == BandPageHeader || band.Kind == BandColumnHeader {
			if err := r.renderBandAt(band, headerY, ctx); err != nil {
				return err
			}
			headerY += r.u(band.Height)
		}
	}

	footerY := r.contentBottom
	for _, band := range r.report.Bands {
		if band.Kind == BandColumnFooter || band.Kind == BandPageFooter {
			if err := r.renderBandAt(band, footerY, ctx); err != nil {
				return err
			}
			footerY += r.u(band.Height)
		}
	}
	return nil
}

func (r *renderer) renderFlowBand(band Band, ctx renderContext) error {
	height := r.u(band.Height)
	bottom := r.contentBottom + pageFitTolerance
	if r.contentTop+height > bottom {
		return fmt.Errorf("band %q is taller than the printable page area", band.ID)
	}
	if r.contentY+height > bottom {
		if err := r.addPage(ctx); err != nil {
			return err
		}
	}
	if err := r.renderBandAt(band, r.contentY, ctx); err != nil {
		return err
	}
	r.contentY += height
	return nil
}

func (r *renderer) renderBandAt(band Band, y float64, ctx renderContext) error {
	x := r.u(r.report.Page.Margin.Left)
	for _, element := range band.Elements {
		if err := r.renderElement(element, x, y, ctx); err != nil {
			return fmt.Errorf("render band %q element %q: %w", band.ID, element.ID, err)
		}
	}
	return nil
}

func (r *renderer) renderElement(element Element, bandX, bandY float64, ctx renderContext) error {
	x := bandX + r.u(element.Rect.X)
	y := bandY + r.u(element.Rect.Y)
	w := r.u(element.Rect.W)
	h := r.u(element.Rect.H)

	if element.Rotation != nil && *element.Rotation != 0 {
		originX, originY := x+w/2, y+h/2
		if element.RotationPoint != nil {
			originX = bandX + r.u(element.RotationPoint.X)
			originY = bandY + r.u(element.RotationPoint.Y)
		}
		r.pdf.TransformBegin()
		r.pdf.TransformRotate(*element.Rotation, originX, originY)
		defer r.pdf.TransformEnd()
	}

	switch element.Type {
	case ElementText:
		return r.renderText(element, x, y, w, h, ctx)
	case ElementLine:
		return r.renderLine(element, x, y)
	case ElementRect:
		return r.renderRectangle(element, x, y, w, h)
	case ElementImage:
		return r.renderImage(element, x, y, w, h)
	case ElementBarcode:
		return r.renderBarcode(element, x, y, w, h, ctx)
	default:
		return fmt.Errorf("unsupported element type %q", element.Type)
	}
}

func (r *renderer) elementText(element Element, ctx renderContext) string {
	if element.Expression != "" {
		return r.expand(element.Expression, ctx)
	}
	if element.Binding != "" {
		return r.resolveBinding(element.Binding, ctx)
	}
	return r.expand(element.Text, ctx)
}

func (r *renderer) renderText(element Element, x, y, w, h float64, ctx renderContext) error {
	text := r.elementText(element, ctx)

	font, unicodeFont := r.fontFamily(element.Style.Font)
	size := r.fontSize(element.Style)
	style := fontStyle(element.Style)
	if !unicodeFont && r.textMode != TextModeWindows1252 {
		if err := r.useBundledFont(style, text); err != nil {
			return err
		}
		font, unicodeFont = bundledFontFamily, true
	}
	if !unicodeFont {
		var encoded strings.Builder
		encoded.Grow(len(text))
		for _, ch := range text {
			value, ok := charmap.Windows1252.EncodeRune(ch)
			if !ok {
				return fmt.Errorf("built-in font %q cannot render %U in windows-1252 mode", font, ch)
			}
			encoded.WriteByte(value)
		}
		text = encoded.String()
	}
	r.pdf.SetFont(font, style, size)
	if err := r.pdf.Error(); err != nil {
		return fmt.Errorf("set font %q: %w", font, err)
	}

	wordWrap := element.WordWrap != nil && *element.WordWrap
	if element.AutoSize != nil && *element.AutoSize {
		for size > 4 && !r.textFits(text, w, h, wordWrap, unicodeFont) {
			size -= 0.5
			r.pdf.SetFont(font, style, size)
		}
	}
	lineHeight := r.pdf.PointConvert(size) * 1.2
	lines := r.textLines(text, w, wordWrap, unicodeFont)
	blockHeight := float64(len(lines)) * lineHeight
	textY := y
	switch element.VerticalAlign {
	case VerticalCenter:
		textY += math.Max(0, (h-blockHeight)/2)
	case VerticalEnd:
		textY += math.Max(0, h-blockHeight)
	}

	fgR, fgG, fgB, _ := parseColor(element.Style.FGColor, 0, 0, 0)
	r.pdf.SetTextColor(fgR, fgG, fgB)
	fill := element.Style.BGColor != "" && element.Style.BGColor != "transparent"
	if fill {
		bgR, bgG, bgB, _ := parseColor(element.Style.BGColor, 255, 255, 255)
		r.pdf.SetFillColor(bgR, bgG, bgB)
		r.pdf.Rect(x, y, w, h, "F")
	}
	r.applyStroke(element.Style)
	border := borderString(element.Style.Border)
	align := textAlignment(element)
	for i, line := range lines {
		r.pdf.SetXY(x, textY+float64(i)*lineHeight)
		r.pdf.CellFormat(w, lineHeight, line, "", 0, align, false, 0, element.URL)
	}
	if border != "" {
		r.drawBorder(x, y, w, h, element.Style)
	}
	return r.pdf.Error()
}

func (r *renderer) renderLine(element Element, x, y float64) error {
	r.applyStroke(element.Style)
	p := element.Points
	r.pdf.Line(x+r.u(p.X1), y+r.u(p.Y1), x+r.u(p.X2), y+r.u(p.Y2))
	if element.Style.StrokeStyle == StrokeDouble {
		offset := r.pdf.PointConvert(1)
		r.pdf.Line(x+r.u(p.X1), y+r.u(p.Y1)+offset, x+r.u(p.X2), y+r.u(p.Y2)+offset)
	}
	return r.pdf.Error()
}

func (r *renderer) renderRectangle(element Element, x, y, w, h float64) error {
	r.applyStroke(element.Style)
	fill := element.Style.BGColor != "" && element.Style.BGColor != "transparent"
	if fill {
		red, green, blue, _ := parseColor(element.Style.BGColor, 255, 255, 255)
		r.pdf.SetFillColor(red, green, blue)
	}
	mode := "D"
	if fill {
		mode = "DF"
	}
	r.pdf.Rect(x, y, w, h, mode)
	if element.Style.StrokeStyle == StrokeDouble {
		inset := r.pdf.PointConvert(1)
		r.pdf.Rect(x+inset, y+inset, math.Max(0, w-2*inset), math.Max(0, h-2*inset), "D")
	}
	return r.pdf.Error()
}

func (r *renderer) renderImage(element Element, x, y, w, h float64) error {
	data, mime, err := elementImageData(element)
	if err != nil {
		return err
	}
	if mime == ImageSVG {
		raster, err := rasterizeSVG(data, w, h, element.ProportionalScaling == nil || *element.ProportionalScaling)
		if err != nil {
			return err
		}
		return r.renderRaster(raster, "PNG", x, y, w, h, false)
	}

	imageType := "PNG"
	if mime == ImageJPEG {
		imageType = "JPG"
	}
	return r.renderRaster(data, imageType, x, y, w, h, element.ProportionalScaling == nil || *element.ProportionalScaling)
}

func rasterizeSVG(data []byte, width, height float64, proportional bool) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse SVG: %w", err)
	}
	pixelWidth := max(1, int(math.Ceil(width*4)))
	pixelHeight := max(1, int(math.Ceil(height*4)))
	targetX, targetY := 0.0, 0.0
	targetW, targetH := float64(pixelWidth), float64(pixelHeight)
	if proportional && icon.ViewBox.W > 0 && icon.ViewBox.H > 0 {
		factor := math.Min(targetW/icon.ViewBox.W, targetH/icon.ViewBox.H)
		targetW, targetH = icon.ViewBox.W*factor, icon.ViewBox.H*factor
		targetX, targetY = (float64(pixelWidth)-targetW)/2, (float64(pixelHeight)-targetH)/2
	}
	icon.SetTarget(targetX, targetY, targetW, targetH)
	canvas := image.NewRGBA(image.Rect(0, 0, pixelWidth, pixelHeight))
	scanner := rasterx.NewScannerGV(pixelWidth, pixelHeight, canvas, canvas.Bounds())
	dasher := rasterx.NewDasher(pixelWidth, pixelHeight, scanner)
	icon.Draw(dasher, 1)
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, fmt.Errorf("encode SVG raster: %w", err)
	}
	return output.Bytes(), nil
}

func (r *renderer) renderRaster(data []byte, imageType string, x, y, w, h float64, proportional bool) error {
	r.resourceID++
	name := fmt.Sprintf("resource-%d", r.resourceID)
	options := fpdf.ImageOptions{ImageType: imageType, ReadDpi: true}
	info := r.pdf.RegisterImageOptionsReader(name, options, bytes.NewReader(data))
	if err := r.pdf.Error(); err != nil {
		return fmt.Errorf("register image: %w", err)
	}
	if proportional && info != nil {
		iw, ih := info.Extent()
		factor := math.Min(w/iw, h/ih)
		drawW, drawH := iw*factor, ih*factor
		x += (w - drawW) / 2
		y += (h - drawH) / 2
		w, h = drawW, drawH
	}
	r.pdf.ImageOptions(name, x, y, w, h, false, options, 0, "")
	return r.pdf.Error()
}

func (r *renderer) renderBarcode(element Element, x, y, w, h float64, ctx renderContext) error {
	content := r.elementText(element, ctx)
	code, err := encodeBarcode(element.BarcodeType, content)
	if err != nil {
		return err
	}
	width := max(1, int(w*8))
	height := max(1, int(h*8))
	scaled, err := barcode.Scale(code, width, height)
	if err != nil {
		return fmt.Errorf("scale barcode: %w", err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, scaled); err != nil {
		return fmt.Errorf("encode barcode image: %w", err)
	}
	return r.renderRaster(data.Bytes(), "PNG", x, y, w, h, false)
}

func (r *renderer) useBundledFont(style, text string) error {
	face := strings.TrimSuffix(style, "U")
	font := r.bundledFonts[face]
	if font == nil {
		data := goregular.TTF
		switch face {
		case "B":
			data = gobold.TTF
		case "I":
			data = goitalic.TTF
		case "BI":
			data = gobolditalic.TTF
		}
		var err error
		font, err = sfnt.Parse(data)
		if err != nil {
			return fmt.Errorf("parse bundled Go font: %w", err)
		}
		r.addUnicodeFont(bundledFontFamily, face, data)
		if err := r.pdf.Error(); err != nil {
			return fmt.Errorf("register bundled Go font: %w", err)
		}
		if r.bundledFonts == nil {
			r.bundledFonts = make(map[string]*sfnt.Font)
		}
		r.bundledFonts[face] = font
	}

	var buffer sfnt.Buffer
	for _, ch := range text {
		switch ch {
		case '\r', '\n', '\t':
			continue
		}
		glyph, err := font.GlyphIndex(&buffer, ch)
		if err != nil {
			return fmt.Errorf("look up bundled Go font glyph %U: %w", ch, err)
		}
		if glyph == 0 {
			return fmt.Errorf("bundled Go font cannot render %U", ch)
		}
	}
	return nil
}

func (r *renderer) addUnicodeFont(family, style string, data []byte) {
	r.pdf.AddUTF8FontFromBytes(family, style, data)
	// shortcut: retain page-count digits via a discarded template, remove when fpdf tracks alias replacement glyphs.
	r.pdf.CreateTemplate(func(tpl *fpdf.Tpl) {
		tpl.SetFont(family, style, 10)
		tpl.Text(0, 0, "0123456789")
	})
}

func (r *renderer) loadFonts() error {
	for name, definition := range r.report.Fonts {
		mime, data, err := decodeDataURI(definition.URL)
		if err != nil {
			return fmt.Errorf("font %q: %w", name, err)
		}
		switch mime {
		case "font/ttf", "application/font-sfnt", "application/x-font-ttf", "application/octet-stream":
		default:
			return fmt.Errorf("font %q: unsupported TrueType MIME type %q", name, mime)
		}
		if !bytes.HasPrefix(data, []byte{0, 1, 0, 0}) && !bytes.HasPrefix(data, []byte("true")) {
			return fmt.Errorf("font %q: expected embedded TrueType font data", name)
		}
		if _, err := sfnt.Parse(data); err != nil {
			return fmt.Errorf("parse TrueType font %q: %w", name, err)
		}
		// A private family name prevents a failed registration from silently
		// picking the built-in font of the same name.
		family := "embedded-" + name
		for _, style := range []string{"", "B", "I", "BI"} {
			r.addUnicodeFont(family, style, data)
			r.pdf.SetFont(family, style, 10)
			if err := r.pdf.Error(); err != nil {
				return fmt.Errorf("register TrueType font %q: %w", name, err)
			}
		}
		r.customFonts[FontName(name)] = true
	}
	return nil
}

func decodeDataURI(value string) (mime string, data []byte, err error) {
	if !strings.HasPrefix(value, "data:") {
		return "", nil, fmt.Errorf("asset must be embedded as a base64 data URI; file and network resources are unsupported")
	}
	header, encoded, found := strings.Cut(value[len("data:"):], ",")
	mime, base64Encoded := strings.CutSuffix(header, ";base64")
	if !found || !base64Encoded || encoded == "" {
		return "", nil, fmt.Errorf("invalid base64 data URI")
	}
	data, err = base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, fmt.Errorf("decode embedded asset: %w", err)
	}
	return mime, data, nil
}

func elementImageData(element Element) ([]byte, ImageMIME, error) {
	var data []byte
	var mime string
	var err error
	switch {
	case element.Image == nil:
		mime, data, err = decodeDataURI(element.URL)
	case strings.HasPrefix(element.Image.Data, "data:"):
		mime, data, err = decodeDataURI(element.Image.Data)
		if err == nil && ImageMIME(mime) != element.Image.MIME {
			return nil, "", fmt.Errorf("image data URI MIME type %q differs from image.mime %q", mime, element.Image.MIME)
		}
	default:
		mime = string(element.Image.MIME)
		data, err = base64.StdEncoding.DecodeString(element.Image.Data)
	}
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	switch ImageMIME(mime) {
	case ImagePNG, ImageJPEG, ImageSVG:
		return data, ImageMIME(mime), nil
	default:
		return nil, "", fmt.Errorf("unsupported image MIME type %q", mime)
	}
}

func (r *renderer) baseContext() renderContext {
	ctx := make(renderContext, len(r.data))
	for name, rows := range r.data {
		if len(rows) > 0 {
			ctx[name] = rows[0]
		} else {
			ctx[name] = map[string]string{}
		}
	}
	return ctx
}

func cloneContext(source renderContext) renderContext {
	clone := make(renderContext, len(source))
	for name, row := range source {
		clone[name] = row
	}
	return clone
}

func bandDataSource(band Band) string {
	if band.DataSource != nil && *band.DataSource != "" {
		return *band.DataSource
	}
	if band.GroupExpr != nil {
		if match := bindingPattern.FindStringSubmatch(*band.GroupExpr); match != nil {
			return match[1]
		}
	}
	for _, element := range band.Elements {
		if source, _, ok := parseBinding(element.Binding); ok {
			return source
		}
		for _, value := range []string{element.Text, element.Expression} {
			if match := bindingPattern.FindStringSubmatch(value); match != nil {
				return match[1]
			}
		}
	}
	return ""
}

func dataBandSection(bands []Band) (count int, source string, grouping *Band) {
	switch bands[0].Kind {
	case BandGroupHeader, BandDetail, BandGroupFooter:
		source = bandDataSource(bands[0])
	}
	if source == "" {
		return 1, "", nil
	}
	for _, kind := range []BandKind{BandGroupHeader, BandDetail, BandGroupFooter} {
		for count < len(bands) && bands[count].Kind == kind {
			band := &bands[count]
			if bandDataSource(*band) != source {
				return count, source, grouping
			}
			if kind != BandDetail {
				if grouping != nil && !sameGrouping(*grouping, *band) {
					return count, source, grouping
				}
				grouping = band
			}
			count++
		}
	}
	return count, source, grouping
}

func groupExpression(band Band) string {
	if band.GroupExpr != nil && strings.TrimSpace(*band.GroupExpr) != "" {
		return *band.GroupExpr
	}
	return ""
}

func sameGrouping(a, b Band) bool {
	left, right := groupExpression(a), groupExpression(b)
	if left != "" || right != "" {
		return left == right
	}
	return slices.Equal(a.GroupBy, b.GroupBy)
}

func (r *renderer) groupRows(band Band, source string, base renderContext) [][]map[string]string {
	ctx := cloneContext(base)
	expression := groupExpression(band)
	positions := make(map[string]int)
	var groups [][]map[string]string
	for _, row := range r.data[source] {
		var key string
		if expression != "" {
			ctx[source] = row
			key = r.expand(expression, ctx)
		} else {
			var fields strings.Builder
			for _, field := range band.GroupBy {
				fields.WriteString(strconv.Quote(row[field]))
			}
			key = fields.String()
		}
		index, seen := positions[key]
		if !seen {
			index = len(groups)
			positions[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], row)
	}
	return groups
}

func parseBinding(value string) (source, column string, ok bool) {
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = value[1 : len(value)-1]
	}
	match := bareBindingPattern.FindStringSubmatch(value)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

func (r *renderer) resolveBinding(binding string, ctx renderContext) string {
	if source, column, ok := parseBinding(binding); ok {
		return ctx[source][column]
	}
	return r.expand(binding, ctx)
}

func (r *renderer) expand(value string, ctx renderContext) string {
	value = strings.ReplaceAll(value, "[PAGE()]", strconv.Itoa(r.pdf.PageNo()))
	value = strings.ReplaceAll(value, "[PAGETOTAL()]", r.pageAlias)
	return bindingPattern.ReplaceAllStringFunc(value, func(token string) string {
		match := bindingPattern.FindStringSubmatch(token)
		return ctx[match[1]][match[2]]
	})
}

func (r *renderer) textFits(text string, width, height float64, wrap, custom bool) bool {
	lines := r.textLines(text, width, wrap, custom)
	size, _ := r.pdf.GetFontSize()
	return float64(len(lines))*r.pdf.PointConvert(size)*1.2 <= height
}

func (r *renderer) textLines(text string, width float64, wrap, custom bool) []string {
	if !wrap {
		return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	}
	result := make([]string, 0)
	for _, explicit := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if explicit == "" {
			result = append(result, "")
		} else if custom {
			result = append(result, r.pdf.SplitText(explicit, width)...)
		} else {
			// Built-in font text has already been converted to the PDF code page.
			for _, line := range r.pdf.SplitLines([]byte(explicit), width) {
				result = append(result, string(line))
			}
		}
	}
	return result
}

func (r *renderer) fontFamily(name FontName) (string, bool) {
	if name == "" && r.report.Defaults != nil {
		name = r.report.Defaults.Font
	}
	if r.customFonts[name] {
		return "embedded-" + string(name), true
	}
	value := strings.ToLower(string(name))
	if strings.Contains(value, "courier") || strings.Contains(value, "consolas") || strings.Contains(value, "mono") || strings.Contains(value, "code") {
		return "Courier", false
	}
	if strings.Contains(value, "times") || strings.Contains(value, "georgia") || strings.Contains(value, "garamond") || strings.Contains(value, "serif") || strings.Contains(value, "merriweather") || strings.Contains(value, "lora") || strings.Contains(value, "playfair") {
		return "Times", false
	}
	return "Helvetica", false
}

func (r *renderer) fontSize(style Style) float64 {
	if style.Size != nil {
		return *style.Size
	}
	if r.report.Defaults != nil && r.report.Defaults.FontSize > 0 {
		return r.report.Defaults.FontSize
	}
	return 10
}

func fontStyle(style Style) string {
	result := ""
	if style.Bold != nil && *style.Bold {
		result += "B"
	}
	if style.Italic != nil && *style.Italic {
		result += "I"
	}
	if style.Underline != nil && *style.Underline {
		result += "U"
	}
	return result
}

func textAlignment(element Element) string {
	if element.HorizontalAlign == HorizontalCenter || element.Style.Align == TextAlignCenter {
		return "C"
	}
	if element.HorizontalAlign == HorizontalEnd || element.Style.Align == TextAlignRight {
		return "R"
	}
	return "L"
}

func borderString(border *Border) string {
	if border == nil {
		return ""
	}
	result := ""
	if border.Left != nil && *border.Left {
		result += "L"
	}
	if border.Top != nil && *border.Top {
		result += "T"
	}
	if border.Right != nil && *border.Right {
		result += "R"
	}
	if border.Bottom != nil && *border.Bottom {
		result += "B"
	}
	return result
}

func (r *renderer) drawBorder(x, y, w, h float64, style Style) {
	border := style.Border
	if border == nil {
		return
	}
	draw := func(inset float64) {
		if border.Left != nil && *border.Left {
			r.pdf.Line(x+inset, y+inset, x+inset, y+h-inset)
		}
		if border.Top != nil && *border.Top {
			r.pdf.Line(x+inset, y+inset, x+w-inset, y+inset)
		}
		if border.Right != nil && *border.Right {
			r.pdf.Line(x+w-inset, y+inset, x+w-inset, y+h-inset)
		}
		if border.Bottom != nil && *border.Bottom {
			r.pdf.Line(x+inset, y+h-inset, x+w-inset, y+h-inset)
		}
	}
	draw(0)
	stroke := style.StrokeStyle
	if border.StrokeStyle != "" {
		stroke = border.StrokeStyle
	}
	if stroke == StrokeDouble {
		draw(r.pdf.PointConvert(1))
	}
}

func (r *renderer) applyStroke(style Style) {
	red, green, blue, _ := parseColor(style.FGColor, 0, 0, 0)
	if style.Border != nil && style.Border.Color != "" {
		red, green, blue, _ = parseColor(style.Border.Color, red, green, blue)
	}
	r.pdf.SetDrawColor(red, green, blue)
	if style.StrokeWidth != nil {
		r.pdf.SetLineWidth(r.pdf.PointConvert(*style.StrokeWidth))
	} else {
		r.pdf.SetLineWidth(r.pdf.PointConvert(1))
	}
	stroke := style.StrokeStyle
	if style.Border != nil && style.Border.StrokeStyle != "" {
		stroke = style.Border.StrokeStyle
	}
	switch stroke {
	case StrokeDotted:
		r.pdf.SetDashPattern([]float64{r.pdf.PointConvert(1), r.pdf.PointConvert(2)}, 0)
	case StrokeDashed:
		r.pdf.SetDashPattern([]float64{r.pdf.PointConvert(4), r.pdf.PointConvert(2)}, 0)
	case StrokeDashDot:
		r.pdf.SetDashPattern([]float64{r.pdf.PointConvert(4), r.pdf.PointConvert(2), r.pdf.PointConvert(1), r.pdf.PointConvert(2)}, 0)
	default:
		r.pdf.SetDashPattern(nil, 0)
	}
}

func parseColor(value string, fallbackR, fallbackG, fallbackB int) (int, int, int, bool) {
	if value == "" || value == "transparent" {
		return fallbackR, fallbackG, fallbackB, value == "transparent"
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 24)
	if err != nil {
		return fallbackR, fallbackG, fallbackB, false
	}
	return int(parsed >> 16), int((parsed >> 8) & 0xff), int(parsed & 0xff), false
}

func (r *renderer) u(value float64) float64 {
	return value * r.scale
}

func encodeBarcode(kind BarcodeType, content string) (barcode.Barcode, error) {
	var (
		code barcode.Barcode
		err  error
	)
	switch kind {
	case BarcodeCODE128, BarcodeCODE128A, BarcodeCODE128B, BarcodeCODE128C:
		code, err = code128.Encode(content)
	case BarcodeEAN13, BarcodeEAN8, BarcodeUPC:
		payloadDigits := 12
		switch kind {
		case BarcodeEAN8:
			payloadDigits = 7
		case BarcodeUPC:
			payloadDigits = 11
		}
		if len(content) != payloadDigits && len(content) != payloadDigits+1 {
			err = fmt.Errorf("expected %d or %d digits", payloadDigits, payloadDigits+1)
			break
		}
		if kind == BarcodeUPC {
			content = "0" + content
		}
		code, err = ean.Encode(content)
	case BarcodeEAN2:
		code, err = encodeEANSupplement(content, 2)
	case BarcodeEAN5:
		code, err = encodeEANSupplement(content, 5)
	case BarcodeCODE39:
		code, err = code39.Encode(content, false, true)
	case BarcodeITF:
		code, err = twooffive.Encode(content, true)
	case BarcodeITF14:
		code, err = encodeITF14(content)
	case BarcodeMSI, BarcodeMSI10, BarcodeMSI11, BarcodeMSI1010, BarcodeMSI1110:
		code, err = encodeMSI(kind, content)
	case BarcodePharmacode:
		code, err = encodePharmacode(content)
	case BarcodeCodabar:
		code, err = codabar.Encode(content)
	default:
		err = fmt.Errorf("unsupported barcode type %q", kind)
	}
	if err != nil {
		return nil, fmt.Errorf("encode %s barcode: %w", kind, err)
	}
	return code, nil
}

func encodeITF14(content string) (barcode.Barcode, error) {
	const payloadDigits = 13
	if (len(content) != payloadDigits && len(content) != payloadDigits+1) || strings.Trim(content, "0123456789") != "" {
		return nil, fmt.Errorf("expected 13 or 14 digits")
	}

	sum := 0
	for i := range payloadDigits {
		weight := 1
		if i%2 == 0 {
			weight = 3
		}
		sum += int(content[i]-'0') * weight
	}
	checkDigit := byte('0' + (10-sum%10)%10)
	if len(content) == payloadDigits {
		content += string(checkDigit)
	} else if content[payloadDigits] != checkDigit {
		return nil, fmt.Errorf("invalid check digit, expected %c", checkDigit)
	}
	return twooffive.Encode(content, true)
}

// bitmapBarcode is a compact barcode.Barcode implementation used for schema
// formats not directly covered by boombuler/barcode.
type bitmapBarcode struct {
	*image.Gray
	content string
	kind    string
}

func (code *bitmapBarcode) Metadata() barcode.Metadata {
	return barcode.Metadata{CodeKind: code.kind, Dimensions: 1}
}

func (code *bitmapBarcode) Content() string { return code.content }

func barcodeFromModules(content, kind string, modules []bool) barcode.Barcode {
	canvas := image.NewGray(image.Rect(0, 0, len(modules), 1))
	for x, black := range modules {
		if black {
			canvas.SetGray(x, 0, color.Gray{Y: 0})
		} else {
			canvas.SetGray(x, 0, color.Gray{Y: 255})
		}
	}
	return &bitmapBarcode{Gray: canvas, content: content, kind: kind}
}

func encodeEANSupplement(content string, digits int) (barcode.Barcode, error) {
	if len(content) != digits || strings.Trim(content, "0123456789") != "" {
		return nil, fmt.Errorf("expected %d digits", digits)
	}
	left := []string{"0001101", "0011001", "0010011", "0111101", "0100011", "0110001", "0101111", "0111011", "0110111", "0001011"}
	group := []string{"0100111", "0110011", "0011011", "0100001", "0011101", "0111001", "0000101", "0010001", "0001001", "0010111"}
	parity := "LL"
	if len(content) == 2 {
		parity = []string{"LL", "LG", "GL", "GG"}[mustAtoi(content)%4]
	} else {
		checksum := 3*(int(content[0]-'0')+int(content[2]-'0')+int(content[4]-'0')) + 9*(int(content[1]-'0')+int(content[3]-'0'))
		parity = []string{"GGLLL", "GLGLL", "GLLGL", "GLLLG", "LGGLL", "LLGGL", "LLLGG", "LGLGL", "LGLLG", "LLGLG"}[checksum%10]
	}
	bits := "1011"
	for i, digit := range content {
		if i > 0 {
			bits += "01"
		}
		value := int(digit - '0')
		if parity[i] == 'G' {
			bits += group[value]
		} else {
			bits += left[value]
		}
	}
	return barcodeFromBitString(content, "EAN supplement", bits), nil
}

func encodeMSI(kind BarcodeType, content string) (barcode.Barcode, error) {
	if content == "" {
		return nil, fmt.Errorf("MSI requires digits")
	}
	if _, err := strconv.Atoi(content); err != nil {
		return nil, fmt.Errorf("MSI requires digits")
	}
	switch kind {
	case BarcodeMSI10:
		content += strconv.Itoa(msiMod10(content))
	case BarcodeMSI11:
		content += msiMod11(content)
	case BarcodeMSI1010:
		content += strconv.Itoa(msiMod10(content))
		content += strconv.Itoa(msiMod10(content))
	case BarcodeMSI1110:
		content += msiMod11(content)
		content += strconv.Itoa(msiMod10(content))
	}
	bits := "110"
	for _, digit := range content {
		value := int(digit - '0')
		for bit := 3; bit >= 0; bit-- {
			if value&(1<<bit) != 0 {
				bits += "110"
			} else {
				bits += "100"
			}
		}
	}
	bits += "1001"
	return barcodeFromBitString(content, "MSI", bits), nil
}

func msiMod10(content string) int {
	sum := 0
	double := true
	for i := len(content) - 1; i >= 0; i-- {
		value := int(content[i] - '0')
		if double {
			value *= 2
			if value > 9 {
				value -= 9
			}
		}
		sum += value
		double = !double
	}
	return (10 - sum%10) % 10
}

func msiMod11(content string) string {
	sum, weight := 0, 2
	for i := len(content) - 1; i >= 0; i-- {
		sum += int(content[i]-'0') * weight
		weight++
		if weight > 7 {
			weight = 2
		}
	}
	check := (11 - sum%11) % 11
	return strconv.Itoa(check)
}

func encodePharmacode(content string) (barcode.Barcode, error) {
	value, err := strconv.Atoi(content)
	if err != nil || value < 3 || value > 131070 {
		return nil, fmt.Errorf("Pharmacode value must be between 3 and 131070")
	}
	widths := make([]int, 0)
	for value > 0 {
		if value%2 == 0 {
			widths = append(widths, 2)
			value = (value - 2) / 2
		} else {
			widths = append(widths, 1)
			value = (value - 1) / 2
		}
	}
	modules := make([]bool, 0)
	for i := len(widths) - 1; i >= 0; i-- {
		for j := 0; j < widths[i]; j++ {
			modules = append(modules, true)
		}
		if i > 0 {
			modules = append(modules, false)
		}
	}
	return barcodeFromModules(content, "Pharmacode", modules), nil
}

func barcodeFromBitString(content, kind, bits string) barcode.Barcode {
	modules := make([]bool, len(bits))
	for i := range bits {
		modules[i] = bits[i] == '1'
	}
	return barcodeFromModules(content, kind, modules)
}

func mustAtoi(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}
