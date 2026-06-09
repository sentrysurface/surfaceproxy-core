# Contributing to SurfaceProxy

First off, thank you for taking the time to contribute! Contributions from the community make projects like SurfaceProxy better for everyone.

This document outlines guidelines and workflows to help you get started with contributing.

---

## Code of Conduct

By participating in this project, you agree to maintain a respectful, welcoming, and collaborative environment.

---

## How Can I Contribute?

### Reporting Bugs

If you find a bug:
1. Search existing issues to ensure it hasn't already been reported.
2. Open a new bug report issue using the provided template.
3. Include clear steps to reproduce, the expected behavior, and logs or error messages.

### Requesting Features

If you want to suggest a new feature or improvement:
1. Check the issue tracker to see if there is similar feedback.
2. Open a feature request issue explaining the use case, why it is valuable, and how it could work.

### Submitting Pull Requests

If you are ready to write code or update documentation:

1. **Fork the Repository**: Create a personal fork on GitHub.
2. **Clone Locally**: Clone your fork to your machine:
   ```bash
   git clone https://github.com/your-username/surfaceproxy-core.git
   cd surfaceproxy-core
   ```
3. **Create a Branch**: Choose a descriptive name for your feature or bug fix branch:
   ```bash
   git checkout -b my-new-feature
   ```
4. **Make Changes**: Implement your changes.
5. **Run Tests**: Verify your changes do not introduce regressions:
   ```bash
   go test ./...
   ```
6. **Ensure Code Quality**: Run `go fmt ./...` and `go vet ./...`. If you use linters, ensure there are no new warnings.
7. **Commit & Push**: Commit your changes with clear, descriptive commit messages, and push your branch:
   ```bash
   git push origin my-new-feature
   ```
8. **Open a Pull Request**: Submit your PR against the `main` branch of the upstream repository.

---

## Development Guidelines

### Go Requirements

- Go version **1.23** or newer is required.
- Follow standard Go style and idioms.

### Dependency Management

If your changes require adding or updating dependencies:
- Do not manually edit `go.sum`.
- Run the dependency verification scripts to regenerate `go.sum` correctly inside the official build environment:
  - **Windows**: `.\scripts\generate-gosum.ps1`
  - **macOS/Linux**: `./scripts/generate-gosum.sh`
- This ensures dependency verification builds succeed in the GitHub CI pipelines.

### Stdio-Mode Log Output

If you add logging to the MCP or application layers:
- In `mcp-mode` (activated via the `mcp-mode` subcommand), all logs **must go to stderr** (`log.SetOutput(os.Stderr)` is automatically set). Standard output (stdout) is strictly reserved for JSON-RPC 2.0 frames. Writing anything else to stdout will corrupt the connection to editors like Cursor or Claude Desktop.
