// Package sniffer ;; decides what an upload is from its first bytes, and whether we store it
package sniffer

import (
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// Kind ;; the class a file belongs to, size and time limits are set per kind
type Kind string

const (
	Image    Kind = "image"
	Video    Kind = "video"
	Audio    Kind = "audio"
	Document Kind = "document"
	Archive  Kind = "archive"
)

var Kinds = []Kind{Image, Video, Audio, Document, Archive}

// HeadSize ;; how many leading bytes Sniff needs
const HeadSize = 3072

var documents = map[string]bool{
	"application/pdf":               true,
	"application/epub+zip":          true,
	"application/msword":            true,
	"application/vnd.ms-excel":      true,
	"application/vnd.ms-powerpoint": true,
	"application/json":              true,
	"text/plain":                    true,
	"text/csv":                      true,
	"text/rtf":                      true,
}

var archives = map[string]bool{
	"application/zip":             true,
	"application/gzip":            true,
	"application/x-tar":           true,
	"application/x-bzip2":         true,
	"application/x-xz":            true,
	"application/zstd":            true,
	"application/x-7z-compressed": true,
	"application/vnd.rar":         true,
}

// Sniff ;; mime type of head and the kind it belongs to. ok is false for types we dont store,
// mime is still set so the caller can say what it saw
func Sniff(head []byte) (mime string, kind Kind, ok bool) {
	mime, _, _ = strings.Cut(mimetype.Detect(head).String(), ";")
	kind, ok = KindOf(mime)
	return mime, kind, ok
}

// KindOf ;; the kind of a mime type, ok is false for types we dont store
func KindOf(mime string) (Kind, bool) {
	switch {
	case mime == "image/svg+xml": // can carry scripts
		return "", false
	case strings.HasPrefix(mime, "image/"):
		return Image, true
	case strings.HasPrefix(mime, "video/"):
		return Video, true
	case strings.HasPrefix(mime, "audio/"), mime == "application/ogg":
		return Audio, true
	case documents[mime],
		strings.HasPrefix(mime, "application/vnd.openxmlformats-officedocument."),
		strings.HasPrefix(mime, "application/vnd.oasis.opendocument."):
		return Document, true
	case archives[mime]:
		return Archive, true
	}
	return "", false
}

// Inline ;; true if a browser may show it in place, everything else is sent as a download
func Inline(mime string) bool {
	kind, ok := KindOf(mime)
	return ok && (kind == Image || kind == Video || kind == Audio || mime == "application/pdf")
}
