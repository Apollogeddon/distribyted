package http

// The pages are templ components (view_*.templ), compiled into view_*_templ.go. The
// generated files are committed, so a plain go build needs no generator.
//go:generate go tool -modfile=../../tools/templ/go.mod templ generate
