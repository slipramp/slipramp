package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

func TestStoreIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT is not set")
	}

	cfg := DefaultConfig()
	cfg.Endpoint = endpoint
	cfg.Region = os.Getenv("TEST_S3_REGION")
	cfg.Bucket = os.Getenv("TEST_S3_BUCKET")
	cfg.AccessKeyID = os.Getenv("TEST_S3_ACCESS_KEY_ID")
	cfg.SecretAccessKey = os.Getenv("TEST_S3_SECRET_ACCESS_KEY")

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := store.Check(ctx); err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	missingConfig := cfg
	missingConfig.Bucket = fmt.Sprintf("slipramp-missing-%d", time.Now().UnixNano())
	missingStore, err := New(missingConfig)
	if err != nil {
		t.Fatalf("New() for missing bucket error = %v", err)
	}

	if err := missingStore.Check(ctx); err == nil {
		t.Fatal("Check() for missing bucket error = nil, want an error")
	}

	key := fmt.Sprintf("integration/%d.txt", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()

		if err := store.Delete(cleanupContext, key); err != nil {
			t.Errorf("Delete() error = %v", err)
		}
	})

	contents := []byte("durable artifact contents")
	written, err := store.Put(
		ctx,
		key,
		bytes.NewReader(contents),
		int64(len(contents)),
		PutOptions{MediaType: "text/plain"},
	)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if written != int64(len(contents)) {
		t.Errorf("Put() size = %d, want %d", written, len(contents))
	}

	object, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := object.Body.Close(); err != nil {
			t.Errorf("object body close error = %v", err)
		}
	}()

	actual, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(actual, contents) {
		t.Errorf("object contents = %q, want %q", actual, contents)
	}

	if object.MediaType != "text/plain" {
		t.Errorf("object media type = %q, want %q", object.MediaType, "text/plain")
	}

	if object.Size != int64(len(contents)) {
		t.Errorf("object size = %d, want %d", object.Size, len(contents))
	}
}
