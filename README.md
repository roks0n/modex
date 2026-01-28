modex
=====

modex is a CLI that traces Python entrypoints and lists referenced models by statically analyzing Python source code.

Build and install
-----------------

Option 1: Go install (recommended)

```
go install ./...
```

Ensure Go bin is on your PATH:

```
export PATH="$PATH:$(go env GOPATH)/bin"
```

Make modex globally available (Linux/macOS)
-------------------------------------------

Add the Go bin directory to your shell profile (bash/zsh):

```
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
```

Then reload your shell and verify:

```
source ~/.bashrc 2>/dev/null || true
source ~/.zshrc 2>/dev/null || true
modex --help
```

Option 2: Build and symlink

```
go build -o modex
sudo ln -sf "$(pwd)/modex" /usr/local/bin/modex
```

On macOS with Homebrew, you can use `/opt/homebrew/bin` instead of `/usr/local/bin`.

Usage
-----

modex traces a Python entrypoint and lists referenced models from static analysis.

```
modex --entrypoint <module-or-path[:object]> [--root <path>] [--explain]
```

Flags
-----

- `--entrypoint` (required): Python module path or file path, optionally with a function or class name.
  - Examples: `pkg.subpkg.module`, `pkg.subpkg.module:MyClass`, `src/pkg/subpkg/module.py:MyClass::method`
- `--root` (optional): Filesystem root of your Python source tree. Defaults to the repository root.
- `--explain` (optional): Show where each model is referenced (module:function).

Examples
--------

List models referenced from a module:

```
modex --entrypoint myapp.analytics.pipeline
```

List models referenced from a specific class or function:

```
modex --entrypoint myapp.analytics.pipeline:MyPipeline
```

List models referenced from a specific class method using a file path:

```
modex --entrypoint src/myapp/analytics/pipeline.py:MyPipeline::run
```

Include usage locations:

```
modex --entrypoint myapp.analytics.pipeline --explain
```
