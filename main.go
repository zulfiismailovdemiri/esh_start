package main

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"esh/esh_vendors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const version = "0.2.0"

func main() {
	_ = godotenv.Load()

	args := os.Args[1:]

	if len(args) == 0 {
		repl()
		return
	}

	switch args[0] {
	case "-h", "--help", "help":
		printHelp()
	case "-v", "--version", "version":
		fmt.Printf("esh %s\n", version)

	case "serve":
		dir := "."
		addr := ":8080"
		if len(args) >= 2 {
			dir = args[1]
		}
		if len(args) >= 3 {
			addr = args[2]
			if !strings.Contains(addr, ":") {
				addr = ":" + addr
			}
		}
		if err := esh_vendors.Serve(dir, addr, version); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

	case "migrate":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: esh migrate <file.sql>")
			os.Exit(1)
		}
		migrateRun(args[1])

	case "migrate:up":
		migrateUp()

	case "migrate:status":
		migrateStatus()

	case "migrate:create":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: esh migrate:create <name>")
			os.Exit(1)
		}
		migrateCreate(args[1])

	case "admin:create-user":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: esh admin:create-user <username> <email>")
			os.Exit(1)
		}
		adminCreateUser(args[1], args[2])

	case "init":
		if err := cmdInit(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

	case "install":
		if err := cmdInstall(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

	case "remove":
		if err := cmdRemove(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

	case "packages":
		if err := cmdPackages(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

	default:
		runFile(args[0])
	}
}

// ---- migrate ----

func connectDB() *sql.DB {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "error: DATABASE_URL not set")
		os.Exit(1)
	}
	if err := esh_vendors.InitDB(dsn); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	esh_vendors.DB.Exec(`CREATE TABLE IF NOT EXISTS _esh_migrations (
		id        SERIAL PRIMARY KEY,
		filename  VARCHAR(255) UNIQUE NOT NULL,
		checksum  VARCHAR(64)  NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	return esh_vendors.DB
}

func migrateRun(path string) {
	db := connectDB()
	name := filepath.Base(path)

	var exists bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM _esh_migrations WHERE filename=$1)", name).Scan(&exists)
	if exists {
		fmt.Printf("  skip   %s (already applied)\n", name)
		return
	}

	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", path, err)
		os.Exit(1)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(src))

	tx, err := db.Begin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: begin tx: %v\n", err)
		os.Exit(1)
	}
	if _, err := tx.Exec(string(src)); err != nil {
		tx.Rollback()
		fmt.Fprintf(os.Stderr, "  FAIL   %s: %v\n", name, err)
		os.Exit(1)
	}
	tx.Exec("INSERT INTO _esh_migrations (filename, checksum) VALUES ($1, $2)", name, checksum)
	if err := tx.Commit(); err != nil {
		fmt.Fprintf(os.Stderr, "error: commit: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  apply  %s\n", name)
}

func migrateUp() {
	db := connectDB()

	entries, err := os.ReadDir("migrations")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: no migrations/ directory found")
		os.Exit(1)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	applied := 0
	for _, name := range files {
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM _esh_migrations WHERE filename=$1)", name).Scan(&exists)
		if exists {
			fmt.Printf("  skip   %s\n", name)
			continue
		}
		src, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", name, err)
			os.Exit(1)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(src))
		tx, _ := db.Begin()
		if _, err := tx.Exec(string(src)); err != nil {
			tx.Rollback()
			fmt.Fprintf(os.Stderr, "  FAIL   %s: %v\n", name, err)
			os.Exit(1)
		}
		tx.Exec("INSERT INTO _esh_migrations (filename, checksum) VALUES ($1, $2)", name, checksum)
		tx.Commit()
		fmt.Printf("  apply  %s\n", name)
		applied++
	}

	if applied == 0 {
		fmt.Println("  nothing to migrate")
	}
}

func migrateStatus() {
	db := connectDB()

	entries, err := os.ReadDir("migrations")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: no migrations/ directory found")
		os.Exit(1)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	fmt.Printf("\n  %-42s  %s\n", "Migration", "Status")
	fmt.Printf("  %-42s  %s\n", strings.Repeat("-", 42), strings.Repeat("-", 10))
	for _, name := range files {
		var appliedAt string
		err := db.QueryRow("SELECT applied_at FROM _esh_migrations WHERE filename=$1", name).Scan(&appliedAt)
		if err == sql.ErrNoRows {
			fmt.Printf("  %-42s  pending\n", name)
		} else {
			fmt.Printf("  %-42s  applied\n", name)
		}
	}
	fmt.Println()
}

func migrateCreate(name string) {
	os.MkdirAll("migrations", 0755)
	stamp := time.Now().Format("20060102150405")
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '_'
	}, name)
	filename := fmt.Sprintf("migrations/%s_%s.sql", stamp, safe)
	body := fmt.Sprintf("-- Migration: %s\n-- Created:   %s\n\n", name, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(filename, []byte(body), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("created %s\n", filename)
}

// ---- admin ----

func adminCreateUser(username, email string) {
	db := connectDB()

	fmt.Print("Password: ")
	reader := bufio.NewReader(os.Stdin)
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	if password == "" {
		fmt.Fprintln(os.Stderr, "error: password cannot be empty")
		os.Exit(1)
	}

	hash, err := esh_vendors.HashPassword(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	_, err = db.Exec(
		`INSERT INTO admin_users (username, email, password_hash)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (username) DO UPDATE SET email=$2, password_hash=$3`,
		username, email, hash,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("admin user '%s' created/updated\n", username)
}

// ---- core ----

func printHelp() {
	fmt.Print(`esh — a small PHP-like language

Usage:
  esh                            start interactive REPL
  esh <file>                     run an .es script
  esh serve [dir] [port]         HTTP server (default: ./ :8080)

  esh init                       create esh.json for this project
  esh install                    install packages listed in esh.json, then build + test
  esh install <pkg>[@<ver>]      add a package to esh.json and install it
  esh remove <pkg>               remove a package from esh.json and rebuild
  esh packages                   list installed packages

  esh migrate <file.sql>         apply one migration file
  esh migrate:up                 apply all pending migrations in migrations/
  esh migrate:status             show applied/pending migrations
  esh migrate:create <name>      create a new migration file
  esh admin:create-user <u> <e>  create or update an admin user
  esh -v                         print version
  esh -h                         show this help

Environment:
  DATABASE_URL   e.g. postgres://user:pass@localhost/mydb?sslmode=disable

Packages:
  esh.json lists required packages; esh.lock records exact resolved versions.
  Packages are Go files (package esh_vendors) fetched from the registry and
  placed in esh_vendors/, then the binary is rebuilt automatically.
  Default registry: https://github.com/zulfiismailovdemiri/esh_repo
`)
}

func runFile(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	env := esh_vendors.NewEnvironment()
	if abs, err := filepath.Abs(path); err == nil {
		env.BaseDir = filepath.Dir(abs)
	}
	if !run(string(src), env, false) {
		os.Exit(1)
	}
}

func repl() {
	fmt.Printf("esh %s — interactive mode (Ctrl-D to exit)\n", version)
	env := esh_vendors.NewEnvironment()
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("esh> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "error reading input: %v\n", err)
			} else {
				fmt.Println()
			}
			return
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		run(line, env, true)
	}
}

func run(src string, env *esh_vendors.Environment, replMode bool) bool {
	src = esh_vendors.PreprocessTemplate(src)
	lexer := esh_vendors.NewLexer(src)
	parser := esh_vendors.NewParser(lexer)
	program := parser.ParseProgram()
	if errs := parser.Errors(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "parse error: %s\n", e)
		}
		return false
	}
	result := esh_vendors.Eval(program, env)
	if esh_vendors.IsError(result) {
		fmt.Fprintln(os.Stderr, result.Inspect())
		return false
	}
	if replMode && result != nil && result.Type() != esh_vendors.OBJ_NULL {
		fmt.Println(result.Inspect())
	}
	return true
}
