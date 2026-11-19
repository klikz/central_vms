// covergate — coverage bo'sagasi tekshiruvi (TASKS §0.4).
//
//	go run ./scripts/covergate -core 70 -all 50 coverage.out
//
// Qoida: faqat internal/ ostidagi paketlar eshib o'lchanadi — cmd/* (main) va
// scripts/* bundan tashqari (ularni unit test bilan qoplash ma'noga ega emas).
// Testi bo'lmagan (statement'lari 0) skelet paketlar ham jazo olmaydi.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	var (
		coreTh   = flag.Int("core", 70, "yadro paketlar uchun minimal coverage (%)")
		allTh    = flag.Int("all", 50, "qolgan paketlar uchun minimal coverage (%)")
		coreList = flag.String("corepkgs", "proto,cams,publish", "yadro paketlar (vergul bilan)")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "ishlatish: covergate [-core N] [-all N] coverage.out\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	core := map[string]bool{}
	for _, p := range strings.Split(*coreList, ",") {
		core[strings.TrimSpace(p)] = true
	}

	type agg struct{ stmts, covered int }
	byPkg := map[string]*agg{}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage faylini ochib bo'lmadi: %v\n", err)
		os.Exit(2)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// format: file:startLine.startCol,endLine.endCol numStmt count
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		filePart := parts[0]
		pkg := pkgOf(filePart)
		var numStmt, count int
		if _, err := fmt.Sscanf(parts[1]+" "+parts[2], "%d %d", &numStmt, &count); err != nil {
			continue
		}
		a := byPkg[pkg]
		if a == nil {
			a = &agg{}
			byPkg[pkg] = a
		}
		a.stmts += numStmt
		if count > 0 {
			a.covered += numStmt
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "o'qish xatosi: %v\n", err)
		os.Exit(2)
	}

	if len(byPkg) == 0 {
		fmt.Fprintln(os.Stderr, "coverage bo'sh — hech qanday paket qamrovda emas")
		os.Exit(1)
	}

	failed := false
	totalStmts, totalCovered := 0, 0
	for pkg, a := range byPkg {
		if a.stmts == 0 {
			continue
		}
		pct := 100 * a.covered / a.stmts
		totalStmts += a.stmts
		totalCovered += a.covered

		name := shortName(pkg)
		if !strings.Contains(pkg, "/internal/") {
			fmt.Printf("%-28s %3d%%  (tashqi: cmd/scripts — tekshirilmaydi)\n", name, 100*a.covered/a.stmts)
			continue
		}
		threshold := *allTh
		if core[name] {
			threshold = *coreTh
		}
		mark := "ok"
		if pct < threshold {
			mark = "PASTA"
			failed = true
		}
		fmt.Printf("%-28s %3d%%  (min %d%%)  %s\n", name, pct, threshold, mark)
	}
	if totalStmts > 0 {
		fmt.Printf("%-28s %3d%%  (%d/%d statement)\n", "UMUMIY", 100*totalCovered/totalStmts, totalCovered, totalStmts)
	}
	if failed {
		fmt.Fprintln(os.Stderr, "coverage bo'sagasi buzilgan")
		os.Exit(1)
	}
	fmt.Println("coverage gate: OK")
}

// pkgOf — "central-vms/client/internal/cams/redact.go" → "central-vms/client/internal/cams"
func pkgOf(file string) string {
	i := strings.LastIndex(file, "/")
	if i < 0 {
		return "."
	}
	return file[:i]
}

func shortName(pkg string) string {
	parts := strings.Split(pkg, "/")
	if len(parts) >= 2 && parts[len(parts)-2] == "internal" {
		return parts[len(parts)-1]
	}
	return parts[len(parts)-1]
}
