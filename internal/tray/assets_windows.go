//go:build windows && !headless

package tray

import _ "embed"

//go:embed assets/icon.ico
var iconBytes []byte
