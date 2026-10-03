// Package imagekit adalah adapter port.ImageStorage untuk ImageKit.
//
// Adapter ini memindahkan logika yang sebelumnya hidup di Cloudflare Worker
// (ARCHITECTURE §9.3): menerbitkan tanda tangan upload dan menghapus berkas.
// Private key hanya dibaca dari konfigurasi server dan tidak pernah dikirim ke
// klien.
package imagekit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"elvan-catalog-api/internal/application/port"
)

const (
	// signatureTTL adalah masa berlaku tanda tangan upload (ARCHITECTURE §9.3).
	signatureTTL = 30 * time.Minute

	// deleteBatchSize adalah jumlah maksimum id per request penghapusan,
	// dibatasi API ImageKit.
	deleteBatchSize = 100

	// defaultBaseURL adalah host API ImageKit.
	defaultBaseURL = "https://api.imagekit.io"

	// maxErrorBody membatasi pembacaan body error agar tidak tak terbatas.
	maxErrorBody = 4 << 10
)

// Client adalah adapter ImageKit.
type Client struct {
	privateKey string
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
	newToken   func() string
}

var _ port.ImageStorage = (*Client)(nil)

// New membuat adapter ImageKit. `privateKey` wajib (divalidasi di config).
func New(privateKey string) *Client {
	return &Client{
		privateKey: privateKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		now:        time.Now,
		newToken:   func() string { return uuid.NewString() },
	}
}

// IssueUploadSignature membuat `{ token, expire, signature }` untuk upload
// langsung dari browser. Kontrak sama persis dengan Worker lama:
// `signature = HMAC-SHA1(privateKey, token + expire)` dalam hex.
func (c *Client) IssueUploadSignature(_ context.Context) (port.UploadSignature, error) {
	token := c.newToken()
	expire := c.now().Add(signatureTTL).Unix()

	mac := hmac.New(sha1.New, []byte(c.privateKey))
	_, _ = io.WriteString(mac, token+strconv.FormatInt(expire, 10))
	signature := hex.EncodeToString(mac.Sum(nil))

	return port.UploadSignature{
		Token:     token,
		Expire:    expire,
		Signature: signature,
	}, nil
}

// deleteRequest adalah body `POST /v1/files/batch/deleteByFileIds`.
type deleteRequest struct {
	FileIDs []string `json:"fileIds"`
}

// deleteResponse merangkum hasil batch delete dari ImageKit.
type deleteResponse struct {
	SuccessfullyDeletedFileIDs []string `json:"successfullyDeletedFileIds"`
}

// Delete menghapus berkas berdasarkan `fileID` dalam batch maksimum 100 id per
// request. Id kosong (gambar lama tanpa fileId) dilewati; id yang gagal
// dikembalikan di `failed` sehingga pemanggil bisa mencatatnya tanpa
// membatalkan operasi DB.
func (c *Client) Delete(ctx context.Context, fileIDs []string) (port.DeleteResult, error) {
	ids := compactIDs(fileIDs)
	if len(ids) == 0 {
		return port.DeleteResult{}, nil
	}

	result := port.DeleteResult{}
	for start := 0; start < len(ids); start += deleteBatchSize {
		end := start + deleteBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]

		deleted, err := c.deleteBatch(ctx, batch)
		if err != nil {
			return result, err
		}
		result.Deleted += len(deleted)
		result.Failed = append(result.Failed, unaccounted(batch, deleted)...)
	}
	return result, nil
}

// deleteBatch mengirim satu request batch dan mengembalikan id yang benar-benar
// dilaporkan terhapus oleh provider.
func (c *Client) deleteBatch(ctx context.Context, ids []string) ([]string, error) {
	body, err := json.Marshal(deleteRequest{FileIDs: ids})
	if err != nil {
		return nil, fmt.Errorf("marshal permintaan hapus: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/files/batch/deleteByFileIds", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("buat permintaan hapus: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.privateKey, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kirim permintaan hapus: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("imagekit menghapus gagal: status %d: %s", resp.StatusCode, string(msg))
	}

	var parsed deleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("baca respons hapus: %w", err)
	}
	return parsed.SuccessfullyDeletedFileIDs, nil
}

// compactIDs membuang string kosong dan duplikat sambil mempertahankan urutan.
func compactIDs(fileIDs []string) []string {
	seen := make(map[string]struct{}, len(fileIDs))
	out := make([]string, 0, len(fileIDs))
	for _, id := range fileIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// unaccounted mengembalikan id batch yang tidak muncul di daftar berhasil
// dihapus. Respons ImageKit tidak menjamin urutan, jadi dibandingkan sebagai
// himpunan.
func unaccounted(batch, deleted []string) []string {
	if len(deleted) == 0 {
		return batch
	}
	done := make(map[string]struct{}, len(deleted))
	for _, id := range deleted {
		done[id] = struct{}{}
	}
	var failed []string
	for _, id := range batch {
		if _, ok := done[id]; !ok {
			failed = append(failed, id)
		}
	}
	return failed
}
