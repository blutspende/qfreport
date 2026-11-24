package render

// Report models the template format described by report.schema.json.
type Report struct {
	Meta        map[string]any        `json:"meta,omitempty"`
	Defaults    *Defaults             `json:"defaults,omitempty"`
	Page        Page                  `json:"page"`
	Fonts       map[string]Font       `json:"fonts"`
	DataSources map[string]DataSource `json:"dataSources"`
	Bands       []Band                `json:"bands"`
}

type Defaults struct {
	Font     FontName `json:"font,omitempty"`
	FontSize float64  `json:"fontSize,omitempty"`
	TextMode TextMode `json:"textMode,omitempty"`
}

type Page struct {
	Size        PageSize    `json:"size"`
	Orientation Orientation `json:"orientation"`
	Unit        Unit        `json:"unit"`
	Width       float64     `json:"width"`
	Height      float64     `json:"height"`
	Margin      Margin      `json:"margin"`
}

type Margin struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

type Font struct {
	URL string `json:"url"`
}

type DataSource struct {
	Kind    DataSourceKind `json:"kind"`
	Columns []Column       `json:"columns"`
}

type Column struct {
	Name string     `json:"name"`
	Type ColumnType `json:"type"`
}

type Band struct {
	ID              string    `json:"id"`
	Kind            BandKind  `json:"kind"`
	Height          float64   `json:"height"`
	EffectiveHeight *float64  `json:"effectiveHeight,omitempty"`
	DataSource      *string   `json:"dataSource,omitempty"`
	GroupBy         []string  `json:"groupBy,omitempty"`
	GroupExpr       *string   `json:"groupExpr,omitempty"`
	Elements        []Element `json:"elements"`
}

// Element represents the schema's tagged union. Fields used by only one
// element kind stay optional and are interpreted according to Type.
type Element struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	Type                ElementType         `json:"type"`
	AutoSize            *bool               `json:"autoSize,omitempty"`
	ProportionalScaling *bool               `json:"proportionalScaling,omitempty"`
	HorizontalAlign     HorizontalAlignment `json:"horizontalAlign,omitempty"`
	VerticalAlign       VerticalAlignment   `json:"verticalAlign,omitempty"`
	WordWrap            *bool               `json:"wordWrap,omitempty"`
	Rotation            *float64            `json:"rotation,omitempty"`
	RotationPoint       *Point              `json:"rotationPoint,omitempty"`
	Rect                Rect                `json:"rect"`
	Style               Style               `json:"style"`
	Binding             string              `json:"binding,omitempty"`
	Text                string              `json:"text,omitempty"`
	Expression          string              `json:"expression,omitempty"`
	URL                 string              `json:"url,omitempty"`
	BarcodeType         BarcodeType         `json:"barcodeType,omitempty"`
	Image               *Image              `json:"image,omitempty"`
	Points              *LinePoints         `json:"points,omitempty"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type LinePoints struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

type Image struct {
	MIME ImageMIME `json:"mime"`
	Data string    `json:"data"`
}

type Style struct {
	Font        FontName    `json:"font,omitempty"`
	Size        *float64    `json:"size,omitempty"`
	Bold        *bool       `json:"bold,omitempty"`
	Italic      *bool       `json:"italic,omitempty"`
	Underline   *bool       `json:"underline,omitempty"`
	Align       TextAlign   `json:"align,omitempty"`
	StrokeWidth *float64    `json:"strokeWidth,omitempty"`
	StrokeStyle StrokeStyle `json:"strokeStyle,omitempty"`
	Border      *Border     `json:"border,omitempty"`
	FGColor     string      `json:"fgColor,omitempty"`
	BGColor     string      `json:"bgColor,omitempty"`
}

type Border struct {
	Top         *bool       `json:"top,omitempty"`
	Right       *bool       `json:"right,omitempty"`
	Bottom      *bool       `json:"bottom,omitempty"`
	Left        *bool       `json:"left,omitempty"`
	Color       string      `json:"color,omitempty"`
	StrokeStyle StrokeStyle `json:"strokeStyle,omitempty"`
}

type PageSize string
type Orientation string
type Unit string
type FontName string
type TextMode string
type DataSourceKind string
type ColumnType string
type BandKind string
type ElementType string
type HorizontalAlignment string
type VerticalAlignment string
type BarcodeType string
type ImageMIME string
type TextAlign string
type StrokeStyle string

const (
	TextModeUnicode     TextMode = "unicode"
	TextModeWindows1252 TextMode = "windows-1252"
)

const (
	PageSizeA6      PageSize = "A6"
	PageSizeA5      PageSize = "A5"
	PageSizeA4      PageSize = "A4"
	PageSizeA3      PageSize = "A3"
	PageSizeLetter  PageSize = "Letter"
	PageSizeLegal   PageSize = "Legal"
	PageSizeTabloid PageSize = "Tabloid"
	PageSizeCustom  PageSize = "custom"
)

const (
	OrientationPortrait  Orientation = "portrait"
	OrientationLandscape Orientation = "landscape"
	UnitMillimeter       Unit        = "mm"
	UnitInch             Unit        = "inch"
	UnitPixel            Unit        = "px"
)

const DataSourceTable DataSourceKind = "table"

const (
	ColumnString     ColumnType = "string"
	ColumnDecimal    ColumnType = "decimal"
	ColumnInteger    ColumnType = "integer"
	ColumnBoolean    ColumnType = "boolean"
	ColumnDate       ColumnType = "date"
	ColumnExpression ColumnType = "expression"
	ColumnUUIDv4     ColumnType = "uuidv4"
)

const (
	BandTitle        BandKind = "title"
	BandReportHeader BandKind = "reportHeader"
	BandPageHeader   BandKind = "pageHeader"
	BandColumnHeader BandKind = "columnHeader"
	BandGroupHeader  BandKind = "groupHeader"
	BandDetail       BandKind = "detail"
	BandChild        BandKind = "child"
	BandGroupFooter  BandKind = "groupFooter"
	BandColumnFooter BandKind = "columnFooter"
	BandPageFooter   BandKind = "pageFooter"
	BandReportFooter BandKind = "reportFooter"
)

const (
	ElementText    ElementType = "text"
	ElementBarcode ElementType = "barcode"
	ElementImage   ElementType = "image"
	ElementLine    ElementType = "line"
	ElementRect    ElementType = "rect"
)

const (
	HorizontalStart  HorizontalAlignment = "start"
	HorizontalCenter HorizontalAlignment = "center"
	HorizontalEnd    HorizontalAlignment = "end"
	VerticalStart    VerticalAlignment   = "start"
	VerticalCenter   VerticalAlignment   = "center"
	VerticalEnd      VerticalAlignment   = "end"
)

const (
	TextAlignLeft   TextAlign   = "left"
	TextAlignCenter TextAlign   = "center"
	TextAlignRight  TextAlign   = "right"
	StrokeSolid     StrokeStyle = "solid"
	StrokeDotted    StrokeStyle = "dotted"
	StrokeDashed    StrokeStyle = "dashed"
	StrokeDashDot   StrokeStyle = "dashdot"
	StrokeDouble    StrokeStyle = "double"
)

const (
	ImagePNG  ImageMIME = "image/png"
	ImageJPEG ImageMIME = "image/jpeg"
	ImageSVG  ImageMIME = "image/svg+xml"
)

const (
	BarcodeCODE128    BarcodeType = "CODE128"
	BarcodeCODE128A   BarcodeType = "CODE128A"
	BarcodeCODE128B   BarcodeType = "CODE128B"
	BarcodeCODE128C   BarcodeType = "CODE128C"
	BarcodeEAN13      BarcodeType = "EAN13"
	BarcodeUPC        BarcodeType = "UPC"
	BarcodeEAN8       BarcodeType = "EAN8"
	BarcodeEAN5       BarcodeType = "EAN5"
	BarcodeEAN2       BarcodeType = "EAN2"
	BarcodeCODE39     BarcodeType = "CODE39"
	BarcodeITF        BarcodeType = "ITF"
	BarcodeITF14      BarcodeType = "ITF-14"
	BarcodeMSI        BarcodeType = "MSI"
	BarcodeMSI10      BarcodeType = "MSI10"
	BarcodeMSI11      BarcodeType = "MSI11"
	BarcodeMSI1010    BarcodeType = "MSI1010"
	BarcodeMSI1110    BarcodeType = "MSI1110"
	BarcodePharmacode BarcodeType = "Pharmacode"
	BarcodeCodabar    BarcodeType = "Codabar"
)
