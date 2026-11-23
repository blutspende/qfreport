# Working on qfreportpdf

## Library contract

- This repository is a standalone Go module. Start with `README.md` for the input format and `qfreportpdf.go` for the public entry point.
- Keep `Generate(templateJSON, dataJSON []byte) ([]byte, error)` in the root package. It returns a complete PDF on success and nil bytes with a contextual error on failure. Keep implementation packages under `internal/`.
- Use `internal/render/report.schema.json` as the template contract. When changing the format, update the schema, Go model, validation, and README together as applicable.
- Bind rows from the caller’s data JSON. Preserve numeric precision, blank cells, and empty-table behavior. Keep rendering in memory using built-in fonts and embedded assets. Database access, file loading, and network requests belong in the calling application.

## Development workflow

- Prefer the smallest correct change and reuse the standard library or existing dependencies.
- Format changed Go files with `gofmt`. Run `go test ./...` and `go vet ./...` from this directory. For layout changes, render a representative PDF and inspect the affected pages.
- Before finishing each development prompt, run `govulncheck ./...` from the module root. Include the result in the final response: no reachable vulnerabilities found, reachable vulnerabilities found (list their Go advisory IDs), or scan failed (give the reason).
