// Package objectstore stores durable artifact contents in S3-compatible storage.
package objectstore

import (
	"io"
	"time"
)

type PutOptions struct {
	MediaType string
}

type Object struct {
	Body      io.ReadCloser
	Size      int64
	MediaType string
}

type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}
