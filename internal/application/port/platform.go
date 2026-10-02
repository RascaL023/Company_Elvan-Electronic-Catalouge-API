package port

import (
	"context"
	"time"
)

// TxManager adalah unit of work: menjalankan fn dalam satu transaksi database.
//
// Implementasi menyimpan transaksi di dalam context sehingga use case tidak
// perlu tahu soal pgx; repository memakai transaksi dari context bila ada.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock memisahkan waktu dari sistem agar use case mudah diuji.
type Clock interface {
	Now() time.Time
}
