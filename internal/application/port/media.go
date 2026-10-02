package port

import "context"

// UploadSignature adalah tanda tangan upload ImageKit yang dikirim ke browser.
type UploadSignature struct {
	Token     string `json:"token"`
	Expire    int64  `json:"expire"`
	Signature string `json:"signature"`
}

// DeleteResult merangkum hasil penghapusan file di provider.
type DeleteResult struct {
	Deleted int      `json:"deleted"`
	Failed  []string `json:"failed,omitempty"`
}

// ImageStorage adalah port penyimpanan gambar (ImageKit hari ini; bisa
// diganti S3/R2). File tidak pernah lewat server: server hanya menerbitkan
// signature dan menghapus file.
type ImageStorage interface {
	IssueUploadSignature(ctx context.Context) (UploadSignature, error)
	Delete(ctx context.Context, fileIDs []string) (DeleteResult, error)
}
