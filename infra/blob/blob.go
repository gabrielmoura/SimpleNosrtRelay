package blob

import (
	"SimpleNosrtRelay/infra/metrics"
	"context"
	"github.com/dgraph-io/badger/v4"
	"github.com/nbd-wtf/go-nostr"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
)

type BlobConfig struct {
	MaxFileSize    int
	MimeAcceptable []string
	ExtAcceptable  []string
	BasePath       string
	AuthRequired   bool
}
type BlobStore struct {
	db *badger.DB
	c  *BlobConfig
}

func NewBlobStore(db *badger.DB, c *BlobConfig) *BlobStore {
	return &BlobStore{db, c}
}
func (bs *BlobStore) StoreBlob(ctx context.Context, sha256 string, body []byte) error {
	metrics.UploadCounter.Inc()
	fp := filepath.Join(bs.c.BasePath, sha256)
	return os.WriteFile(fp, body, 0644)
}
func (bs *BlobStore) LoadBlob(ctx context.Context, sha256 string) (io.ReadSeeker, error) {
	metrics.DownloadCounter.Inc()
	fp := filepath.Join(bs.c.BasePath, sha256)
	return os.Open(fp)
}
func (bs *BlobStore) DeleteBlob(ctx context.Context, sha256 string) error {
	fp := filepath.Join(bs.c.BasePath, sha256)
	return os.Remove(fp)
}
func (bs *BlobStore) Init() error {
	// verificar se a pasta blobs existe, caso não exista, criar
	if _, err := os.Stat(bs.c.BasePath); os.IsNotExist(err) {
		err := os.MkdirAll(bs.c.BasePath, 0755)
		if err != nil {
			log.Println("Erro ao criar pasta de blobs")
			return err
		}
	}
	return nil
}
func (bs *BlobStore) RejectUpload(funcUserAllow func(auth *nostr.Event) bool) func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
	return func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if bs.c.AuthRequired {
			if funcUserAllow(auth) {
				return false, "", 0
			}
			return true, "user not allowed to upload", 403
		}
		if size > bs.c.MaxFileSize {
			return true, "file too big", 413
		}

		if !slices.Contains(bs.c.ExtAcceptable, ext) {
			return true, "file type not supported", 415
		}

		return true, "", 0
	}
}
