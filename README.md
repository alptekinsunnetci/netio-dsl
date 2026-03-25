# Netio DSL v1

Netio DSL is a **human-readable, machine-producible, domain-agnostic configuration language**.
Originally designed for network devices, it can also be used for general configuration, environment files, application settings, and policies.

---

## 🚀 Features

- Human-friendly and easy to write
- Deterministic: same input → same output
- Machine-friendly and easy to parse
- Block and key-value based, no `{}` or `end` required
- Support for include, reuse, and extension
- Lists and nested blocks supported
- Minimal syntax, diff-friendly, canonical formatting possible

---

## 🔹 Basic Syntax

### 1. Block
```
IDENT [STRING] :
    statement*
```

### 2. Key-Value
```
key = value
```
- VALUE → int, float, string, bool, ip/cidr, list  
- STRING → `"..."` or single word (quotes required if spaces exist)  

### 3. Child / Chaining (optional)
```
block -> child-block:
    key = value
```

### 4. List
```
list-name:
  - item1
  - item2
```

### 5. Comment
```
# This is a comment
```

### 6. Include / Reuse
```
include "base.config"

app myservice -> extends base-app:
  version = "1.2.3"
```

---

## 🔹 Example Configs

### 1. Simple Application Config
```
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

### 2. Logging Config
```
logging:
  level = "info"
  output -> file:
    path = "/var/log/myservice.log"
  output -> console:
    enabled = true
```

### 3. Multi-Environment Config
```
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

### 4. Nested Lists
```
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

### 5. Policy / Rules
```
policy enforce-ssl:
  if env != "dev":
    action = "require-https"

policy rate-limit:
  max_requests = 1000
  period = "1h"
```

---

## 🔹 Advantages

- **Readable:** Humans can easily write and understand configs
- **Machine-friendly:** Systems can generate configs programmatically
- **Domain-agnostic:** Works for network, environment, application, or policy configs
- **Include/Extend:** Reuse and inheritance of configs
- **Minimal:** Fewer symbols, diff-friendly
- **Canonical:** Formatter can enforce a single standard style

---

## 🔹 Syntax Summary

| Element         | Syntax                             |
|-----------------|-----------------------------------|
| Block           | `IDENT [STRING] :`                 |
| Key-Value       | `key = value`                      |
| Child Block     | `block -> child-block:`            |
| List            | `list-name:` + `- item`            |
| Comment         | `# comment`                        |
| Include / Reuse | `include "file"` / `block -> extends base` |

---

## 🔹 Next Steps

- Go parser + AST implementation
- Validation engine
- Compiler to JSON / ENV / network devices
- Formatter and linter

---

> **Note:** This DSL is designed for a balance between human editing and programmatic generation. It does not include complex expressions or Turing-complete features to maintain readability and deterministic parsing.

