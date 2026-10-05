package httpapi

import (
	"time"

	"elvan-catalog-api/internal/domain"
)

// isoMillisFormat menyamai `Date.prototype.toISOString()` di FE:
// "2006-01-02T15:04:05.000Z" (selalu UTC, milidetik).
const isoMillisFormat = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(isoMillisFormat)
}

// maxRequestBody membatasi ukuran body JSON (ARCHITECTURE §13).
const maxRequestBody = 1 << 20 // 1 MiB

// adminDTO adalah admin yang sedang login (dipakai /auth/me dan respons login).
// `passwordHash` tidak pernah ikut.
type adminDTO struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"createdAt"`
}

func toAdminDTO(a domain.Admin) adminDTO {
	return adminDTO{ID: a.ID, Email: a.Email, CreatedAt: formatTime(a.CreatedAt)}
}

// sessionDTO adalah respons login: admin + masa berlaku sesi.
type sessionDTO struct {
	Admin     adminDTO `json:"admin"`
	ExpiresAt string   `json:"expiresAt"`
}

type ratingDTO struct {
	Rate  float64 `json:"rate"`
	Count int     `json:"count"`
}

// catalogProductDTO adalah proyeksi ringan yang sama dengan `CatalogProduct` FE.
type catalogProductDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Price     int64     `json:"price"`
	Category  string    `json:"category"`
	Brand     string    `json:"brand"`
	Thumbnail string    `json:"thumbnail"`
	Rating    ratingDTO `json:"rating"`
	IsActive  bool      `json:"isActive"`
	CreatedAt string    `json:"createdAt"`
}

func toCatalogProductDTO(p domain.CatalogProduct) catalogProductDTO {
	return catalogProductDTO{
		ID:        p.ID,
		Name:      p.Name,
		Slug:      p.Slug,
		Price:     p.Price,
		Category:  p.Category,
		Brand:     p.Brand,
		Thumbnail: p.Thumbnail,
		Rating:    ratingDTO{Rate: p.Rating.Rate, Count: p.Rating.Count},
		IsActive:  p.IsActive,
		CreatedAt: formatTime(p.CreatedAt),
	}
}

func toCatalogProductDTOs(products []domain.CatalogProduct) []catalogProductDTO {
	out := make([]catalogProductDTO, 0, len(products))
	for _, p := range products {
		out = append(out, toCatalogProductDTO(p))
	}
	return out
}

// imageDTO adalah bentuk gambar di API: array nested, bukan array paralel FE.
// Adapter FE memetakan ke `images: string[]` + `imageFileIds: string[]`.
type imageDTO struct {
	Key    string `json:"key"`
	FileID string `json:"fileId"`
}

// productDTO adalah produk lengkap (halaman detail/edit).
type productDTO struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Price       int64      `json:"price"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Brand       string     `json:"brand"`
	Images      []imageDTO `json:"images"`
	Rating      ratingDTO  `json:"rating"`
	IsActive    bool       `json:"isActive"`
	CreatedAt   string     `json:"createdAt"`
	UpdatedAt   string     `json:"updatedAt"`
}

func toProductDTO(p *domain.Product) productDTO {
	images := make([]imageDTO, 0, len(p.Images))
	for _, img := range p.Images {
		images = append(images, imageDTO{Key: img.Key, FileID: img.FileID})
	}
	return productDTO{
		ID:          p.ID,
		Name:        p.Name,
		Slug:        p.Slug,
		Price:       p.Price,
		Description: p.Description,
		Category:    p.Category,
		Brand:       p.Brand,
		Images:      images,
		Rating:      ratingDTO{Rate: p.Rating.Rate, Count: p.Rating.Count},
		IsActive:    p.IsActive,
		CreatedAt:   formatTime(p.CreatedAt),
		UpdatedAt:   formatTime(p.UpdatedAt),
	}
}

// productListDTO adalah hasil `list()` yang sama dengan `ProductListResult` FE.
// `cursor` sengaja pointer agar menjadi `null` (bukan string kosong) di JSON.
type productListDTO struct {
	Products []catalogProductDTO `json:"products"`
	HasMore  bool                `json:"hasMore"`
	Cursor   *string             `json:"cursor"`
}

type categoryDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

func toCategoryDTO(c domain.Category) categoryDTO {
	return categoryDTO{
		ID:          c.ID,
		Name:        c.Name,
		Slug:        c.Slug,
		Description: c.Description,
		CreatedAt:   formatTime(c.CreatedAt),
		UpdatedAt:   formatTime(c.UpdatedAt),
	}
}

type brandDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func toBrandDTO(b domain.Brand) brandDTO {
	return brandDTO{
		ID:        b.ID,
		Name:      b.Name,
		Slug:      b.Slug,
		CreatedAt: formatTime(b.CreatedAt),
		UpdatedAt: formatTime(b.UpdatedAt),
	}
}
