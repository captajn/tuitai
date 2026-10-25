//go:build !windows && !linux && !darwin

package main

// term_other.go: nền tảng Unix khác, không đổi chế độ terminal.

func termSetup() (func(), error) { return func() {}, nil }

func termWidth() int {
	if w := envInt("COLUMNS"); w > 0 {
		return w
	}
	return 100
}
