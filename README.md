# esh_start

The official starter template for building ESH projects.

Clone this repository, declare your packages in `esh.json`, and run
`esh install` once. Everything else — the language runtime, the HTTP server,
the database layer — is pulled from
[esh_repo](https://github.com/zulfiismailovdemiri/esh_repo) and compiled
into a self-contained binary on your machine.

No `esh_vendors/` directory is committed. It is generated on demand, the same
way `vendor/` works in PHP Composer projects.

---

## What is in this repository

| File           | Purpose                                                  | Commit? |
|----------------|----------------------------------------------------------|---------|
| `main.go`      | CLI entry point — REPL, file runner, all subcommands     | yes     |
| `installer.go` | Package manager — `esh install / remove / packages`      | yes     |
| `go.mod`       | Go module declaration + external dependency versions     | yes     |
| `go.sum`       | Go dependency checksums (locked, do not edit manually)   | yes     |
| `esh.json`     | Your package list (edit this to add/remove packages)     | yes     |
| `esh.lock`     | Auto-generated exact version lock (like composer.lock)   | yes     |
| `esh_vendors/` | Downloaded package source files — **do not commit**      | no      |
| `esh`          | Compiled binary — **do not commit**                      | no      |

---

## Prerequisites

- **Go 1.21+** — [install](https://go.dev/dl/)

No existing `esh` binary is required. The `bootstrap/` command handles
everything from a fresh clone using only the Go toolchain.

---

## Starting a new project

### Step 1 — Clone this template

```sh
git clone https://github.com/zulfiismailovdemiri/esh_start.git my-project
cd my-project
```

Remove the template's git history and start your own:

```sh
rm -rf .git
git init
```

### Step 2 — Edit esh.json

Open `esh.json` and set your project name and description. The `require` block
already lists all core ESH packages — remove the ones you do not need or add
community packages from `esh_repo`.

```json
{
  "name": "my-project",
  "version": "1.0.0",
  "description": "My ESH web application",
  "require": {
    "token":       "main",
    "lexer":       "main",
    "ast":         "main",
    "object":      "main",
    "environment": "main",
    "parser":      "main",
    "evaluator":   "main",
    "builtins":    "main",
    "template":    "main",
    "server":      "main",
    "db":          "main"
  }
}
```

The version value is a **git ref** — a branch name (`"main"`) or a release
tag (`"v1.2.0"`). Using `"main"` always tracks the latest version of a package.

### Step 3 — Bootstrap

Run the self-contained bootstrap command. It requires only Go — no existing
`esh` binary needed:

```sh
go run ./bootstrap/
```

This will:
1. Read `esh.json`
2. Download every listed package from
   [esh_repo](https://github.com/zulfiismailovdemiri/esh_repo) into `esh_vendors/`
3. Write `esh.lock` with exact checksums
4. Run `go build -o esh .` to compile the binary

Once you have `./esh`, install it globally so you can use `esh install` for
future package changes:

```sh
sudo cp esh /usr/local/bin/esh
```

### Step 4 — Start building

```sh
# Run the interactive REPL
./esh

# Run a script
./esh hello.es

# Start the HTTP server
./esh serve ./public 8080
```

Create your application files in `public/` (or any directory you prefer).
ESH files use the `.es` extension and support PHP-like template tags:

```php
<?es
$name = "World";
?>
<h1>Hello, <?= $name ?>!</h1>
```

---

## Package management day-to-day

```sh
# Install all packages listed in esh.json (first run or after editing esh.json)
esh install

# Add a new package and install it immediately
esh install hello@main

# Pin a package to a specific release tag
esh install my_extension@v1.2.0

# Remove a package (deletes its files from esh_vendors/ and rebuilds)
esh remove hello

# List installed packages
esh packages
```

### How packages work

Every package in `esh_repo` is a directory containing:

- `package.json` — manifest (name, version, files list)
- One or more `.go` files declaring `package esh_vendors`

When you run `esh install`, the `.go` files are downloaded into `esh_vendors/`
and the binary is rebuilt. Because packages are compiled Go code, there is no
runtime overhead and the compiler catches type errors at build time.

To write your own package and publish it, create a directory in
[esh_repo](https://github.com/zulfiismailovdemiri/esh_repo) following this
structure:

```
my_pkg/
├── package.json
└── my_pkg.go        ← must declare: package esh_vendors
```

Register new built-in functions via `RegisterBuiltin` inside `init()`:

```go
package esh_vendors

func init() {
    RegisterBuiltin("slugify", func(env *Environment, args ...Object) Object {
        if len(args) != 1 {
            return &Error{Message: "slugify expects 1 argument"}
        }
        s := strings.ToLower(args[0].Inspect())
        s = strings.ReplaceAll(s, " ", "-")
        return &String{Value: s}
    })
}
```

---

## Project layout (recommended)

```
my-project/
├── main.go           ← do not edit (from esh_start)
├── installer.go      ← do not edit (from esh_start)
├── go.mod            ← do not edit manually
├── go.sum            ← do not edit manually
├── esh.json          ← your package list
├── esh.lock          ← auto-generated, commit this
├── .gitignore
├── .env              ← environment variables (never commit)
│
├── public/           ← your application
│   ├── index.es      ← entry point / front controller
│   ├── routes/
│   ├── controllers/
│   ├── models/
│   ├── layouts/
│   ├── components/
│   └── css/
│
└── migrations/       ← SQL migration files
    └── 001_init.sql
```

---

## Database setup

Set `DATABASE_URL` in your `.env` file:

```
DATABASE_URL=postgres://user:pass@localhost:5432/mydb?sslmode=disable
```

Create and apply migrations:

```sh
esh migrate:create create_users_table
esh migrate:up
esh migrate:status
```

Create an admin user:

```sh
esh admin:create-user alice alice@example.com
```

---

## Updating packages

To get the latest version of all packages on the `main` branch, delete
`esh.lock` and re-run `esh install`:

```sh
rm esh.lock
esh install
```

To pin to a specific release tag, update the version in `esh.json`:

```json
"evaluator": "v2.0.0"
```

Then run `esh install` to apply.

---

## Links

- [esh_repo](https://github.com/zulfiismailovdemiri/esh_repo) — package registry
- [esh](https://github.com/zulfiismailovdemiri/esh) — language reference and source
