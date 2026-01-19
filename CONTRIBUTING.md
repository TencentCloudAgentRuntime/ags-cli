# Contributing to AGS CLI

[中文版](CONTRIBUTING-zh.md)

Thank you for your interest in contributing to AGS CLI! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How to Contribute](#how-to-contribute)
  - [Reporting Bugs](#reporting-bugs)
  - [Suggesting Features](#suggesting-features)
  - [Submitting Design Proposals](#submitting-design-proposals)
  - [Submitting Pull Requests](#submitting-pull-requests)
- [Development Setup](#development-setup)
- [Coding Guidelines](#coding-guidelines)
- [Commit Messages](#commit-messages)
- [Review Process](#review-process)

## Code of Conduct

This project follows the [CNCF Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## How to Contribute

### Reporting Bugs

Before creating a bug report, please check existing issues to avoid duplicates.

When filing a bug report, please include:

1. **Title**: A clear and descriptive title
2. **Environment**: OS, Go version, AGS CLI version
3. **Steps to Reproduce**: Detailed steps to reproduce the issue
4. **Expected Behavior**: What you expected to happen
5. **Actual Behavior**: What actually happened
6. **Logs/Screenshots**: Any relevant logs or screenshots
7. **Additional Context**: Any other relevant information

Use the bug report template when creating an issue.

### Suggesting Features

We welcome feature suggestions! When proposing a new feature:

1. **Check Existing Issues**: Search for similar feature requests first
2. **Create an Issue**: Use the feature request template
3. **Describe the Feature**: Explain the feature clearly
4. **Use Cases**: Describe the use cases and benefits
5. **Alternatives**: List any alternatives you've considered

### Submitting Design Proposals

For significant changes or new features, we recommend submitting a design proposal first:

1. **Create a Design Document**: Write a design document covering:
   - Problem statement
   - Proposed solution
   - Technical design details
   - API changes (if any)
   - Migration plan (if applicable)
   - Testing strategy

2. **Submit as an Issue**: Create an issue with the `design-proposal` label

3. **Discussion**: Engage with maintainers and community feedback

4. **Approval**: Wait for maintainer approval before implementation

### Submitting Pull Requests

1. **Fork the Repository**: Fork and clone the repository

2. **Create a Branch**: Create a feature branch from `main`
   ```bash
   git checkout -b feature/your-feature-name
   ```

3. **Make Changes**: Implement your changes following our coding guidelines

4. **Write Tests**: Add or update tests as needed

5. **Run Tests**: Ensure all tests pass
   ```bash
   make test
   ```

6. **Commit Changes**: Follow our commit message guidelines

7. **Push Changes**: Push to your fork
   ```bash
   git push origin feature/your-feature-name
   ```

8. **Create PR**: Open a pull request with:
   - Clear title and description
   - Reference to related issues
   - Summary of changes
   - Testing performed

## Development Setup

### Prerequisites

- Go 1.21 or later
- Make

### Building from Source

```bash
# Clone the repository
git clone https://github.com/TencentCloudAgentRuntime/ags-cli.git
cd ags-cli

# Build
make build

# Run tests
make test

# Install locally
make install
```

### Project Structure

```
ags-cli/
├── cmd/           # Command implementations
├── internal/      # Internal packages
├── examples/      # Usage examples
├── build/         # Build artifacts
└── main.go        # Entry point
```

## Coding Guidelines

### Go Style

- Follow [Effective Go](https://golang.org/doc/effective_go) guidelines
- Use `gofmt` for formatting
- Run `go vet` before committing
- Keep functions focused and concise

### Error Handling

- Always handle errors explicitly
- Provide meaningful error messages
- Use wrapped errors for context

### Documentation

- Document all exported functions and types
- Keep comments up to date with code changes
- Include examples where helpful

## Commit Messages

Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

### Examples

```
feat(run): add support for Ruby language

fix(instance): resolve timeout handling issue

docs(readme): update installation instructions
```

## Review Process

1. **Automated Checks**: All PRs must pass CI checks
2. **Code Review**: At least one maintainer approval required
3. **Testing**: Adequate test coverage expected
4. **Documentation**: Update docs if needed
5. **Merge**: Maintainers will merge approved PRs

### Review Timeline

- Initial response: Within 3 business days
- Review completion: Depends on PR complexity

## Questions?

If you have questions, feel free to:

- Open a discussion issue
- Reach out to maintainers

Thank you for contributing to AGS CLI!
