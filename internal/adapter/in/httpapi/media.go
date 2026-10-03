package httpapi

import (
	"context"
	"net/http"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// MediaIssuer adalah use case media yang dipakai adapter HTTP.
type MediaIssuer interface {
	IssueUploadSignature(ctx context.Context) (port.UploadSignature, error)
	DeleteImages(ctx context.Context, fileIDs []string) (port.DeleteResult, error)
}

// signatureResponse adalah body `GET /api/v1/media/signature`. Bentuknya sama
// persis dengan respons Worker lama agar FE tidak perlu berubah.
type signatureResponse struct {
	Token     string `json:"token"`
	Expire    int64  `json:"expire"`
	Signature string `json:"signature"`
}

// deleteFilesRequest adalah body `DELETE /api/v1/media/files`.
type deleteFilesRequest struct {
	FileIDs []string `json:"fileIds"`
}

// mediaSignature: GET /api/v1/media/signature — tanda tangan upload ImageKit.
// Wajib sesi admin (dijaga `requireAdmin`), bukan hanya cek `Origin` seperti
// Worker lama.
func (h *handlers) mediaSignature(w http.ResponseWriter, r *http.Request) {
	if h.deps.Media == nil {
		writeError(w, h.deps.Log, r, domain.ErrNotFound)
		return
	}
	sig, err := h.deps.Media.IssueUploadSignature(r.Context())
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, signatureResponse{
		Token:     sig.Token,
		Expire:    sig.Expire,
		Signature: sig.Signature,
	})
}

// mediaDeleteFiles: DELETE /api/v1/media/files — penghapusan berkas eksplisit.
// Dipertahankan sementara untuk kompatibilitas FE selama transisi ke
// pembersihan server-side (ARCHITECTURE §11); nanti dihapus setelah FE berhenti
// memanggilnya.
func (h *handlers) mediaDeleteFiles(w http.ResponseWriter, r *http.Request) {
	if h.deps.Media == nil {
		writeError(w, h.deps.Log, r, domain.ErrNotFound)
		return
	}

	var req deleteFilesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	if len(req.FileIDs) == 0 {
		writeError(w, h.deps.Log, r, validationError("fileIds", "tidak boleh kosong"))
		return
	}

	result, err := h.deps.Media.DeleteImages(r.Context(), req.FileIDs)
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, result)
}
