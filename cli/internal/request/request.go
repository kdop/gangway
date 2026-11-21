// Package request holds the request an AI sends, as every piece of the
// cli-tool sees it.
package request

// Request is one thing an AI asks the host to do.
type Request struct {
	ID      string
	Command string
}
