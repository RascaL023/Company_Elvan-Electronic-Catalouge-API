// Package clock menyediakan implementasi port.Clock berbasis waktu sistem.
package clock

import (
	"time"

	"elvan-catalog-api/internal/application/port"
)

// Real adalah Clock yang memakai time.Now. Use case menggantung pada
// port.Clock sehingga test bisa menyuntikkan waktu statis.
type Real struct{}

var _ port.Clock = Real{}

// Now mengembalikan waktu sistem saat ini.
func (Real) Now() time.Time { return time.Now() }
