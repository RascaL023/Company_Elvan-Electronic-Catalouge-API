package imagekit

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestIssueUploadSignatureMatchesImageKitContract(t *testing.T) {
	fixed := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c := New("private_test_key")
	c.now = func() time.Time { return fixed }
	c.newToken = func() string { return "token-abc" }

	sig, err := c.IssueUploadSignature(context.Background())
	if err != nil {
		t.Fatalf("IssueUploadSignature: %v", err)
	}

	if sig.Token != "token-abc" {
		t.Errorf("token = %q", sig.Token)
	}
	if want := fixed.Add(30 * time.Minute).Unix(); sig.Expire != want {
		t.Errorf("expire = %d, ingin %d", sig.Expire, want)
	}

	mac := hmac.New(sha1.New, []byte("private_test_key"))
	_, _ = mac.Write([]byte("token-abc" + strconv.FormatInt(sig.Expire, 10)))
	if want := hex.EncodeToString(mac.Sum(nil)); sig.Signature != want {
		t.Errorf("signature = %q, ingin %q", sig.Signature, want)
	}
}

func TestDeleteSkipsEmptyIDs(t *testing.T) {
	c := New("key")
	called := false
	c.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}

	res, err := c.Delete(context.Background(), []string{"", ""})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if called {
		t.Error("tidak boleh memanggil provider bila tidak ada fileId")
	}
	if res.Deleted != 0 {
		t.Errorf("deleted = %d, ingin 0", res.Deleted)
	}
}

func TestDeleteSendsBatchAndCountsDeleted(t *testing.T) {
	var gotBody deleteRequest
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(deleteResponse{
			SuccessfullyDeletedFileIDs: []string{"f1", "f2"},
		})
	}))
	defer srv.Close()

	c := New("private_key")
	c.baseURL = srv.URL

	res, err := c.Delete(context.Background(), []string{"f1", "f2", "f1", ""})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if res.Deleted != 2 {
		t.Errorf("deleted = %d, ingin 2", res.Deleted)
	}
	if len(res.Failed) != 0 {
		t.Errorf("failed = %v, ingin kosong", res.Failed)
	}
	if len(gotBody.FileIDs) != 2 {
		t.Errorf("fileIds terkirim = %v, ingin duplikat & kosong dibuang", gotBody.FileIDs)
	}
	if gotAuth == "" {
		t.Error("request harus memakai Basic auth dengan private key")
	}
}

func TestDeleteReportsUnaccountedAsFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(deleteResponse{SuccessfullyDeletedFileIDs: []string{"f1"}})
	}))
	defer srv.Close()

	c := New("key")
	c.baseURL = srv.URL

	res, err := c.Delete(context.Background(), []string{"f1", "f2"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("deleted = %d, ingin 1", res.Deleted)
	}
	if len(res.Failed) != 1 || res.Failed[0] != "f2" {
		t.Errorf("failed = %v, ingin [f2]", res.Failed)
	}
}

func TestDeleteReturnsErrorOnProviderFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New("key")
	c.baseURL = srv.URL

	if _, err := c.Delete(context.Background(), []string{"f1"}); err == nil {
		t.Fatal("Delete seharusnya mengembalikan error pada status non-2xx")
	}
}

// roundTripFunc memungkinkan menyuntikkan transport palsu.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
