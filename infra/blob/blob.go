// Package blob provides functionality for storing, retrieving, and managing binary large objects (blobs).
// It includes configuration options for maximum file size, acceptable MIME types and extensions,
// and authentication requirements.
package blob

import (
	"SimpleNosrtRelay/infra/log"
	"SimpleNosrtRelay/infra/metrics"
	"context"
	"github.com/dgraph-io/badger/v4"
	"github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// BlobConfig holds the configuration for the blob store.
type BlobConfig struct {
	BasePath       string
	MimeAcceptable []string
	ExtAcceptable  []string
	MaxFileSize    int
	AuthRequired   bool
}

// BlobStore represents a store for binary large objects.
type BlobStore struct {
	db *badger.DB
	c  *BlobConfig
}

// NewBlobStore creates a new BlobStore instance.
func NewBlobStore(db *badger.DB, c *BlobConfig) *BlobStore {
	return &BlobStore{db, c}
}

// StoreBlob stores a blob with the given SHA256 hash as its file name.
func (bs *BlobStore) StoreBlob(ctx context.Context, sha256 string, body []byte) error {
	metrics.UploadCounter.Inc()
	fp := filepath.Join(bs.c.BasePath, sha256)
	return os.WriteFile(fp, body, 0644)
}

// LoadBlob retrieves a blob based on its SHA256 hash.
// It returns an io.ReadSeeker for reading the blob.
func (bs *BlobStore) LoadBlob(ctx context.Context, sha256 string) (io.ReadSeeker, error) {
	metrics.DownloadCounter.Inc()
	fp := filepath.Join(bs.c.BasePath, sha256)
	return os.Open(fp)
}

// DeleteBlob deletes a blob based on its SHA256 hash.
func (bs *BlobStore) DeleteBlob(ctx context.Context, sha256 string) error {
	fp := filepath.Join(bs.c.BasePath, sha256)
	return os.Remove(fp)
}

// Init initializes the blob storage, creating the base directory if it doesn't exist.
func (bs *BlobStore) Init() error {
	// Check if the blobs directory exists; if not, create it.
	if _, err := os.Stat(bs.c.BasePath); os.IsNotExist(err) {
		err := os.MkdirAll(bs.c.BasePath, 0755)
		if err != nil {
			log.Logger.Error("Error creating blob directory:", zap.Error(err))
			return err
		}
	}
	return nil
}

// RejectUpload returns a function that determines if a blob upload should be rejected
// based on the configuration. It checks for authentication, file size, and extension.
// funcUserAllow: callback function to check if the user is allowed to upload
func (bs *BlobStore) RejectUpload(funcUserAllow func(auth *nostr.Event) bool) func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
	return func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		// Check if authentication is required.
		if bs.c.AuthRequired {
			// If authentication is required and the user is not allowed, reject the upload.
			if !funcUserAllow(auth) {
				return true, "restricted: user not allowed to upload", 403
			}
			return false, "", 0 // User is authenticated, continue
		}

		// Check if the file size exceeds the maximum allowed size.
		if size > bs.c.MaxFileSize {
			return true, "file too big", 413
		}
		// Check if the file extension is in the list of allowed extensions.
		if !slices.Contains(bs.c.ExtAcceptable, ext) {
			return true, "file type not supported", 415
		}

		return false, "", 0
	}
}
