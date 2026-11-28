// Package fmdapi serves the FileMaker Data API at /fmi/data/{version} (#208).
//
// The goal is wire compatibility with FileMaker Server: the status, the
// FileMaker code, the message text, the key order and the number spelling
// of every reply are the ones FileMaker Server sends, as recorded by the
// conformance fixtures in testdata/dataapi.
//
// FileMaker Server answers in two layers, and the replies tell them apart:
//
//   - its Node.js front end (Express, body-parser and a request validator)
//     checks routes, methods, content types, JSON syntax and parameters. Its
//     errors put "message" before "code";
//   - the Data API engine behind it opens files, checks accounts and runs
//     requests. Its errors put "code" before "message".
//
// This package reproduces both: a request goes through the front-end checks
// in their order before a handler runs. Phase P1 serves productInfo,
// databases, sign-in, sign-out and validateSession; phase P3 the layout and
// script menus and the description of a layout (metadata.go), from the
// layout contract (schema). Every other endpoint is routed and validated
// like the others, then refused with code 3 until its phase lands.
//
// Names stay at this boundary: a FileMaker database name maps to a
// File4Base database through the configured aliases, and SQL identifiers
// never see it.
package fmdapi
