# Netio DSL v1

Netio DSL is a **human-readable, machine-producible, domain-agnostic configuration language**.

It is designed for scenarios where configs must be:
- easy for humans to edit,
- safe and deterministic for machines to generate,
- composable (reuse + inheritance),
- diff-friendly (stable canonical formatting).

> Status (2026-03-25): This repository currently provides the **language specification** and examples. Parser/AST/compiler are planned (see Roadmap).

---

## Why another config language?

Netio DSL focuses on **deterministic composition** and **path-based addressing** of nested data, without introducing complex expression syntax.

### Key differentiators

1) **Path chaining (`->`) for nested addressing**
- You can set nested values or create nested blocks using a single line:
  - `database -> host = "db.example.com"`
  - `logging -> output -> file: ...`

2) **Deterministic inheritance (`extends`)**
- Reuse configuration via overlay semantics:
  - `app myservice -> extends base-app:`
- Clear, deterministic merge rules (documented below).

3) **Include as a dependency graph**
- `include "base.config"` loads dependencies first, then applies current file as overlay.
- Enables modular config design.

4) **Canonical / stable formatting**
- The same input AST can be rendered into a canonical form with stable ordering to produce clean diffs.

5) **Minimal syntax**
- Block + key-value + lists + comments.
- No `{}` and no `end` keywords.

---

## Syntax Overview

### 1) Blocks

A block starts with a **head** and a trailing `:`.

```text
IDENT [NAME] :
  statement*
```

Examples:

```text
app myservice:
  version = "1.2.3"
```

```text
logging:
  level = "info"
```

### 2) Key-Value assignments

```text
key = value
```

- `value` can be: `int`, `float`, `string`, `bool`, `ip/cidr` (planned), lists, and bare words.
- `string` is either `"quoted string"` or a single bare word (quotes required if spaces exist).

Example:

```text
replicas = 3
enabled = true
host = "db.example.com"
```

### 3) Path chaining (`->`) — nested addressing

Chaining is used to address nested paths.

#### Chained block
```text
logging -> output -> file:
  path = "/var/log/myservice.log"
```

Equivalent to:
```text
logging:
  output:
    file:
      path = "/var/log/myservice.log"
```

#### Chained assignment
```text
database -> host = "prod-db.example.com"
```

Equivalent to:
```text
database:
  host = "prod-db.example.com"
```

**Rule (recommended):**
- When a chained assignment references intermediate blocks that do not exist, they are **created implicitly**.

### 4) Lists

```text
list-name:
  - item1
  - item2
```

Example:

```text
features:
  - enable-login
  - enable-cache
```

### 5) Comments

```text
# This is a comment
```

---

## Include & Composition

### Include

```text
include "base.config"
```

**Semantics (recommended):**
- Includes are processed first (depth-first).
- After included files are loaded, the current file is applied as an overlay.
- Include cycles must be detected and treated as an error.

### Inheritance via `extends`

```text
app myservice -> extends base-app:
  version = "1.2.3"
```

**Recommended deterministic merge rules**
When `X extends Y`, interpret it as: **start from Y, then overlay X**.

- Scalar keys: `X` overrides `Y`
- Nested blocks: recursively merge
- Lists: **replace** by default (simple and deterministic)

Notes:
- `extends` cycles must be detected and treated as an error.
- The lookup of `Y` should be defined (same file first, then included files, etc.).

---

## Examples

### 1) Simple Application Config

```text
app myservice:
  version = "1.2.3"
  replicas = 3

  database:
    host = "db.example.com"
    port = 5432
    user = "app"
    password = "secret"

  features:
    - enable-login
    - enable-cache
```

### 2) Logging Config (with chaining)

```text
logging:
  level = "info"

logging -> output -> file:
  path = "/var/log/myservice.log"

logging -> output -> console:
  enabled = true
```

### 3) Multi-Environment Config (with include + chaining)

```text
include "base.env"

environment production:
  app myservice:
    replicas = 5
    database -> host = "prod-db.example.com"

environment staging:
  app myservice:
    replicas = 2
    database -> host = "staging-db.example.com"
```

### 4) Nested Lists

```text
servers:
  - name = "web-1"
    ip = "10.0.0.1"
    roles:
      - web
      - cache
  - name = "web-2"
    ip = "10.0.0.2"
    roles:
      - web
```

---

## Determinism & Canonical Formatting

Netio DSL aims to be deterministic:
- The parser produces a stable AST.
- A formatter can output a **canonical representation** with stable ordering.
- This improves diffs and enables reliable config generation.

**Canonical formatting (recommended):**
- keys sorted alphabetically inside a block
- blocks sorted alphabetically inside a block
- list item order preserved

---

## Roadmap

- Go lexer + parser + AST implementation
- Validation engine (schema-like constraints)
- Compilers/emitters:
  - JSON
  - ENV
  - network device formats (optional)
- Formatter and linter
- Include/extends cycle detection + diagnostics

---

## Non-goals

To keep parsing deterministic and configs readable, Netio DSL avoids:
- Turing-complete features
- complex expression evaluation
- ambiguous implicit behavior (all composition rules should be explicit)

---

## License

MIT License (see `LICENSE`).
