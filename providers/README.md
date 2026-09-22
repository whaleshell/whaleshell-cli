# Providers (local copies)

Canonical builtin profiles live in **`whaleshell-providers/profiles/`**.

This directory keeps copies for older docs/paths; prefer importing from:

```bash
whaleshell provider profile import ../whaleshell-providers/profiles/github.yaml
```

`provider.FindBuiltinDir()` resolves `whaleshell-providers/profiles` first.
