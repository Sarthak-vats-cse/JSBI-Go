# JSBI-Go

> A Go implementation of the JSBI BigInt API, preserving JavaScript BigInt behavior through Go's native arbitrary-precision integer support.

<p align="center">
  <img src="https://img.shields.io/badge/Language-Go-00ADD8?style=for-the-badge&logo=go" />
  <img src="https://img.shields.io/badge/Source-JSBI-yellow?style=for-the-badge" />
  <img src="https://img.shields.io/badge/License-Apache%202.0-green?style=for-the-badge" />
</p>

---

## 📖 Overview

**JSBI-Go** is a migration of the JavaScript **JSBI** library to **Go** as part of the **Port Marathon 2026**.

The project preserves the external behavior and developer-facing API of JSBI while adapting the implementation to Go's language features and standard library.

Instead of reproducing JavaScript's internal 30-bit limb representation, this migration leverages Go's `math/big.Int` to provide arbitrary-precision arithmetic while maintaining JSBI-compatible semantics for arithmetic, parsing, comparisons, bitwise operations, shifts, and integer conversions.

---

## 🎯 Original Repository

This project is based on the official JSBI implementation.

**Source Repository**

https://github.com/GoogleChromeLabs/jsbi

**Migrated Source File**

```
lib/jsbi.ts
```

The migration focuses on preserving the observable behavior of the original library while implementing it idiomatically in Go.

---

# ✨ Features

- ✅ Arbitrary Precision Integer Arithmetic
- ✅ JavaScript BigInt Compatible API
- ✅ Addition
- ✅ Subtraction
- ✅ Multiplication
- ✅ Division
- ✅ Remainder
- ✅ Exponentiation
- ✅ Bitwise Operations
- ✅ Left & Right Shift Operations
- ✅ String Parsing
- ✅ Multi-Radix Support
- ✅ DataView Integer Operations
- ✅ asIntN / asUintN
- ✅ Float64 Conversion
- ✅ Comprehensive Unit Tests

---

# 🏗 Migration Strategy

JavaScript requires JSBI because it historically lacked native arbitrary-precision integers.

Go already provides arbitrary-precision integers through the standard library (`math/big`).

Instead of recreating JSBI's internal 30-bit arithmetic implementation, this migration preserves the public API behavior while mapping operations onto Go's arbitrary-precision integer implementation.

This approach:

- Preserves observable behavior
- Reduces implementation complexity
- Improves maintainability
- Aligns with Go best practices
- Takes advantage of the language's standard library

---

# 📂 Repository Structure

```
JSBI-Go/
│
├── go.mod
├── jsbi.go
├── jsbi_test.go
└── README.md
```

---

# ⚙️ Requirements

- Go 1.26 or later

---

# 🚀 Installation

Clone the repository

```bash
git clone https://github.com/Sarthak-vats-cse/JSBI-Go.git
```

Open the project

```bash
cd JSBI-Go
```

---

# ▶ Running Tests

```bash
go test -v
```

---

# 📊 Coverage

```bash
go test -cover
```

---

# ⚡ Benchmarks

```bash
go test -bench=.
```

---

# 🧪 Testing

The project includes unit tests covering:

- Arithmetic Operations
- Truncating Division
- Bitwise Operations
- Shift Operations
- Parsing
- String Conversion
- Float64 Conversion
- DataView Operations
- Mixed Operators
- asIntN
- asUintN

---

# 📚 Technologies Used

- Go 1.26
- Go Standard Library
- math/big
- Go Testing Package

---

# 🎥 Demo

The demonstration video included with the submission shows:

- Successful project build
- Complete test execution
- Passing unit tests
- Repository structure
- Go implementation

---

# 👨‍💻 Team

**Team Name**

**NULL_POINTERS**

---

# 📄 License

This project is released under the Apache 2.0 License, consistent with the licensing of the original JSBI project.

---

# 🙏 Acknowledgements

Special thanks to the maintainers of the original JSBI project for creating a robust arbitrary-precision integer library for JavaScript.

Original Project:

https://github.com/GoogleChromeLabs/jsbi

---

## Port Marathon 2026

This repository was developed as part of **Port Marathon 2026**, demonstrating language migration from **TypeScript** to **Go** while preserving the functionality and semantics of the original project.
