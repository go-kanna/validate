module github.com/go-kanna/validate/example

go 1.27.0

toolchain go1.27.3

tool (
	github.com/go-kanna/validate/cmd/kanna-validate
	github.com/go-kanna/value/cmd/kanna-value
)

require (
	github.com/go-kanna/validate v0.0.12
	github.com/go-kanna/value v0.0.9
)

require (
	github.com/go-kanna/internal v0.0.16 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)
