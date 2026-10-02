// Package application berisi use case. Paket ini hanya boleh bergantung pada
// domain dan paket application lain; port (interface) didefinisikan di sini.
package application

// Actor adalah pelaku sebuah request. Untuk saat ini hanya membedakan admin
// terautentikasi dari pengunjung anonim; itulah yang menentukan apakah
// `includeInactive` dihormati.
type Actor struct {
	IsAdmin bool
}

// Anonymous adalah actor tanpa sesi (pengunjung publik).
var Anonymous = Actor{}
