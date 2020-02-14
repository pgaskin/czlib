package czlib

//go:generate go run -tags zlib_generate zlib_generate.go

// #cgo CPPFLAGS:
// #cgo LDFLAGS:
import "C"
