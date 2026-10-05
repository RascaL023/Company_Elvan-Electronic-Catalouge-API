// Package media berisi use case modul media: menerbitkan tanda tangan upload
// dan menghapus berkas gambar di provider (ARCHITECTURE §11).
//
// Berkas tidak pernah melewati server: browser mengunggah langsung ke ImageKit
// dengan tanda tangan dari `IssueUploadSignature`, lalu server hanya menyimpan
// `{ key, fileId }` dan membersihkan berkas yang terlepas.
package media

import (
	"context"
	"log/slog"

	"elvan-catalog-api/internal/application/port"
)

// Service adalah use case media.
type Service struct {
	storage port.ImageStorage
	log     *slog.Logger
}

// New membuat Service media. `storage` adalah adapter provider (ImageKit
// hari ini); `log` boleh nil.
func New(storage port.ImageStorage, log *slog.Logger) *Service {
	return &Service{storage: storage, log: log}
}

// IssueUploadSignature menerbitkan tanda tangan upload. Penjagaan sesi admin
// ada di adapter HTTP — berbeda dari Worker lama yang hanya mengecek `Origin`.
func (s *Service) IssueUploadSignature(ctx context.Context) (port.UploadSignature, error) {
	return s.storage.IssueUploadSignature(ctx)
}

// DeleteImages menghapus berkas di provider. Dijalankan **setelah** transaksi
// database commit supaya kegagalan di provider tidak membatalkan perubahan DB
// (ARCHITECTURE §11). Kegagalan dicatat di log dengan `fileId`-nya.
func (s *Service) DeleteImages(ctx context.Context, fileIDs []string) (port.DeleteResult, error) {
	result, err := s.storage.Delete(ctx, fileIDs)
	if err != nil {
		if s.log != nil {
			s.log.ErrorContext(ctx, "hapus berkas gambar di provider gagal",
				"file_ids", fileIDs,
				"error", err.Error(),
			)
		}
		return result, err
	}
	if len(result.Failed) > 0 && s.log != nil {
		s.log.WarnContext(ctx, "sebagian berkas gambar tidak terhapus",
			"failed", result.Failed,
			"deleted", result.Deleted,
		)
	}
	return result, nil
}
