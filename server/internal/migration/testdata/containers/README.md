# Container storage fixture

A database created on 2026-11-24 with FileMaker Pro 26.0.2 (Windows, French
system settings), used by `container_storage_test.go` to check how native
`.fmp12` containers migrate when FileMaker embeds them, keeps them in open
external storage, or keeps them in secure external storage.

## Contents

| Path | What it is |
| --- | --- |
| `f4b-containers.fmp12.gz` | The database, gzip-compressed. `gunzip` gives FileMaker's file byte for byte. |
| `f4b-containers.xml.gz` | Its Save a Copy as XML (FMSaveAsXML 2.3.0.0), gzip-compressed. |
| `f4b-containers/` | The external storage folder FileMaker wrote beside the database, unchanged. |
| `source/fields-1.xml`, `source/fields-2.xml` | The field definitions pasted into Manage Database to build it. |

Uncompressed SHA-256:

```text
b1ddcb48d5c4323a1451f2f907cf79022367e44902d23ed5b3c96c78cbf4b3a4  f4b-containers.fmp12
74eb6d5ff83ba2ee218923952d37fcb03d8923aa2d90db9d327d63cecbbc0f44  f4b-containers.xml
a071de099929052cb11945537cb0aff2095ffe85529f712f5461a74bc6862540  f4b-containers/Table/external_file/readme.txt
cffff4845a87eff37327ae6ec2f96613279c751be030a7cdc4837f21387b4a62  f4b-containers/Secure/3C/F1/D9A6B4E4/983D06F6/131D4032/420B_129
```

## How it was made

1. New blank file (FileMaker's five default fields included), table `Table`.
2. Fields pasted from `source/fields-1.xml`: `label` (Text) and four Container
   fields, each filled on creation by `Base64Decode(…; "<name>")` with one
   payload file below.
3. `external_file` set to **open** external storage (`Table/external_file/`,
   relative to the default `Files/f4b-containers/` base); the others stay embedded.
4. Record 1 created. Field `external_secure` pasted from `source/fields-2.xml`
   and set to **secure** external storage. Record 2 created; record 1 has no
   value in `external_secure`. The blank record FileMaker creates with a new
   file was deleted.

Each embedded payload was exported back with Edit > Export Field Contents and
matched the hashes below byte for byte. FileMaker stored `readme.txt` once for
both records. The secure-storage file is encrypted: 48 bytes on disk for the
44-byte payload.

| Field | Payload | Bytes | SHA-256 |
| --- | --- | --- | --- |
| `image_png` | `gradient.png` | 6321 | `8c12b7425ca2c5cbc9cfa733fbab6f2bd267a379baaec01e0fecb2d2ab6b154f` |
| `image_jpeg` | `pattern.jpg` | 2265 | `75a946f55133bcb5d8666963fb1e88b6127556182f1b0bebb2ffe2ce84ca907c` |
| `document_pdf` | `note.pdf` | 601 | `f1dbbd5a4454237fcfe10ef66ad126441c3afe3f93de29833056f5bdd1c43f94` |
| `external_file` | `readme.txt` | 46 | `a071de099929052cb11945537cb0aff2095ffe85529f712f5461a74bc6862540` |
| `external_secure` | `secure.txt` | 44 | `6119e69d3d71e1af8c51aed7d284e246bb67447bae8667d2d51482b098663d87` |

## Observed storage descriptors

What the `.fmp12` keeps for an externally stored container, as read by
`externalStorageContainer`:

- `file` fork: original size (uint64, little-endian), MD5 of the file as
  written to disk (plaintext for open storage, ciphertext for secure storage),
  stored file name, NUL.
- `REMO` fork (open storage): 8 zero bytes, folder relative to the base
  directory, NUL.
- `ENCR` fork (secure storage): 32 bytes; the file cannot be read without
  FileMaker.

These are observations from this one file, not a published format.

## Provenance

Original work, contributed under the repository's GPL-3.0-only terms. The
payloads were generated for this fixture (a synthetic gradient, a pattern, a
one-page PDF and two text notes); no vendor sample, customer data, screenshot,
licence or credential is included. The only account is FileMaker's default
`Admin`; field comments are FileMaker's French defaults. Built and verified by
Claude Code (Claude Opus 5.5) in a disposable, network-isolated virtual machine,
driving FileMaker Pro through Windows UI Automation.
