package postgres

import (
	"errors"
	"testing"
	"time"

	"elvan-catalog-api/internal/domain"
)

func TestCursorRoundTrip(t *testing.T) {
	created := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	price := int64(1500000)
	rate := 4.5
	count := int32(12)

	cases := []cursorPayload{
		{Sort: domain.SortDefault, CreatedAt: &created, ID: "018f-aaa"},
		{Sort: domain.SortPriceAsc, Price: &price, ID: "018f-bbb"},
		{Sort: domain.SortPriceDesc, Price: &price, ID: "018f-ccc"},
		{Sort: domain.SortRatingDesc, RatingRate: &rate, RatingCount: &count, ID: "018f-ddd"},
	}

	for _, want := range cases {
		got, err := decodeCursor(encodeCursor(want))
		if err != nil {
			t.Fatalf("decode gagal untuk %+v: %v", want, err)
		}
		if got.Sort != want.Sort || got.ID != want.ID {
			t.Errorf("round-trip sort/id salah: got %+v, want %+v", got, want)
		}
		if want.CreatedAt != nil && (got.CreatedAt == nil || !got.CreatedAt.Equal(*want.CreatedAt)) {
			t.Errorf("created_at tidak sama: got %v, want %v", got.CreatedAt, want.CreatedAt)
		}
		if want.Price != nil && (got.Price == nil || *got.Price != *want.Price) {
			t.Errorf("price tidak sama: got %v, want %v", got.Price, want.Price)
		}
		if want.RatingRate != nil && (got.RatingRate == nil || *got.RatingRate != *want.RatingRate) {
			t.Errorf("rating_rate tidak sama: got %v, want %v", got.RatingRate, want.RatingRate)
		}
		if want.RatingCount != nil && (got.RatingCount == nil || *got.RatingCount != *want.RatingCount) {
			t.Errorf("rating_count tidak sama: got %v, want %v", got.RatingCount, want.RatingCount)
		}
	}
}

func TestDecodeCursorEmpty(t *testing.T) {
	got, err := decodeCursor("")
	if err != nil {
		t.Fatalf("cursor kosong seharusnya tanpa error, dapat %v", err)
	}
	if got != nil {
		t.Fatalf("cursor kosong seharusnya nil, dapat %+v", got)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	inputs := []string{
		"!!!bukan-base64!!!",
		"bm90LWpzb24", // "not-json"
		encodeCursor(cursorPayload{Sort: "bogus", ID: "x"}),           // sort tidak dikenal
		encodeCursor(cursorPayload{Sort: domain.SortDefault, ID: ""}), // tanpa id
	}

	for _, in := range inputs {
		if _, err := decodeCursor(in); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("decodeCursor(%q) seharusnya ErrValidation, dapat %v", in, err)
		}
	}
}
