package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"elvan-catalog-api/internal/application/catalog"
	"elvan-catalog-api/internal/application/taxonomy"
	"elvan-catalog-api/internal/domain"
)

// CatalogWriter adalah use case tulis produk yang dipakai adapter HTTP.
type CatalogWriter interface {
	Create(ctx context.Context, in catalog.CreateProductInput) (*domain.Product, error)
	Update(ctx context.Context, id string, patch domain.ProductPatch) (*domain.Product, error)
	Delete(ctx context.Context, id string) error
}

// TaxonomyWriter adalah use case tulis kategori & brand.
type TaxonomyWriter interface {
	CreateCategory(ctx context.Context, in taxonomy.CreateCategoryInput) (*domain.Category, error)
	UpdateCategory(ctx context.Context, id string, patch domain.CategoryPatch) (*domain.Category, error)
	DeleteCategory(ctx context.Context, id string) error

	CreateBrand(ctx context.Context, in taxonomy.CreateBrandInput) (*domain.Brand, error)
	UpdateBrand(ctx context.Context, id string, patch domain.BrandPatch) (*domain.Brand, error)
	DeleteBrand(ctx context.Context, id string) error
}

// --- produk -----------------------------------------------------------------

// createProductRequest adalah payload POST /products. `price` pointer supaya
// field yang hilang ketahuan (bukan diam-diam jadi 0); `id` dari klien
// diabaikan — id selalu dibuat server.
type createProductRequest struct {
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Price       *int64     `json:"price"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Brand       string     `json:"brand"`
	Images      []imageDTO `json:"images"`
	Rating      *ratingDTO `json:"rating"`
	IsActive    *bool      `json:"isActive"`
}

// updateProductRequest memakai pointer agar bisa membedakan "tidak dikirim"
// (jangan diubah) dari "dikirim" (ubah). `images: []` mengganti seluruh gambar
// dengan daftar kosong.
type updateProductRequest struct {
	Name        *string     `json:"name"`
	Slug        *string     `json:"slug"`
	Price       *int64      `json:"price"`
	Description *string     `json:"description"`
	Category    *string     `json:"category"`
	Brand       *string     `json:"brand"`
	Images      *[]imageDTO `json:"images"`
	Rating      *ratingDTO  `json:"rating"`
	IsActive    *bool       `json:"isActive"`
}

func (h *handlers) createProduct(w http.ResponseWriter, r *http.Request) {
	var req createProductRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	if req.Price == nil {
		writeError(w, h.deps.Log, r, validationError("price", "wajib diisi"))
		return
	}

	in := catalog.CreateProductInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Price:       *req.Price,
		Description: req.Description,
		Category:    req.Category,
		Brand:       req.Brand,
		Images:      toDomainImages(req.Images),
		IsActive:    true,
	}
	if req.Rating != nil {
		in.Rating = domain.Rating{Rate: req.Rating.Rate, Count: req.Rating.Count}
	}
	if req.IsActive != nil {
		in.IsActive = *req.IsActive
	}

	created, err := h.deps.CatalogWriter.Create(r.Context(), in)
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, toProductDTO(created))
}

func (h *handlers) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}

	var req updateProductRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	patch := domain.ProductPatch{
		Name:        req.Name,
		Slug:        req.Slug,
		Price:       req.Price,
		Description: req.Description,
		Category:    req.Category,
		Brand:       req.Brand,
		IsActive:    req.IsActive,
	}
	if req.Images != nil {
		images := toDomainImages(*req.Images)
		patch.Images = &images
	}
	if req.Rating != nil {
		rating := domain.Rating{Rate: req.Rating.Rate, Count: req.Rating.Count}
		patch.Rating = &rating
	}

	updated, err := h.deps.CatalogWriter.Update(r.Context(), id, patch)
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, toProductDTO(updated))
}

func (h *handlers) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}

	if err := h.deps.CatalogWriter.Delete(r.Context(), id); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- kategori ---------------------------------------------------------------

type createCategoryRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

type updateCategoryRequest struct {
	Name        *string `json:"name"`
	Slug        *string `json:"slug"`
	Description *string `json:"description"`
}

func (h *handlers) createCategory(w http.ResponseWriter, r *http.Request) {
	var req createCategoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	created, err := h.deps.TaxonomyWriter.CreateCategory(r.Context(), taxonomy.CreateCategoryInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, toCategoryDTO(*created))
}

func (h *handlers) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}

	var req updateCategoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	updated, err := h.deps.TaxonomyWriter.UpdateCategory(r.Context(), id, domain.CategoryPatch{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, toCategoryDTO(*updated))
}

func (h *handlers) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}

	if err := h.deps.TaxonomyWriter.DeleteCategory(r.Context(), id); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- brand ------------------------------------------------------------------

type createBrandRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type updateBrandRequest struct {
	Name *string `json:"name"`
	Slug *string `json:"slug"`
}

func (h *handlers) createBrand(w http.ResponseWriter, r *http.Request) {
	var req createBrandRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	created, err := h.deps.TaxonomyWriter.CreateBrand(r.Context(), taxonomy.CreateBrandInput{
		Name: req.Name,
		Slug: req.Slug,
	})
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, toBrandDTO(*created))
}

func (h *handlers) updateBrand(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}

	var req updateBrandRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	updated, err := h.deps.TaxonomyWriter.UpdateBrand(r.Context(), id, domain.BrandPatch{
		Name: req.Name,
		Slug: req.Slug,
	})
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, toBrandDTO(*updated))
}

func (h *handlers) deleteBrand(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}

	if err := h.deps.TaxonomyWriter.DeleteBrand(r.Context(), id); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- bantuan ----------------------------------------------------------------

// pathUUID memastikan `{id}` di path berupa UUID canonical. Id legacy Firestore
// hanya berlaku untuk baca (GET); operasi tulis harus UUID (issue #6), sehingga
// nilai lain ditolak sebagai `400 validation_failed` field `id`.
func pathUUID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, nil, r, validationError("id", "harus UUID canonical"))
		return "", false
	}
	return id, true
}

func toDomainImages(in []imageDTO) []domain.Image {
	out := make([]domain.Image, 0, len(in))
	for _, img := range in {
		out = append(out, domain.Image{Key: img.Key, FileID: img.FileID})
	}
	return out
}
