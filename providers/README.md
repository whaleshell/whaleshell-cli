# Providers (local copies)

Canonical builtin profiles live in **`osg-providers/profiles/`**.

This directory keeps copies for older docs/paths; prefer importing from:

```bash
osg provider profile import ../osg-providers/profiles/github.yaml
```

`provider.FindBuiltinDir()` resolves `osg-providers/profiles` first.
