package main

// gui_assets.go — nhúng giao diện vào binary (không cần file ngoài khi phát hành).

import _ "embed"

//go:embed gui_index.html
var guiHTML string
