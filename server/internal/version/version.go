// Package version — build-da ldflags orqali beriladigan versiya ma'lumotlari
// (root REJA §4.3: -X main.version=... Makefile'da internal/version ga yo'naltiriladi).
package version

var (
	Version = "dev"
	Commit  = "none"
	BuiltAt = ""
)
