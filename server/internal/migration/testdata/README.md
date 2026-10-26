# Migration test fixtures

## `recipes_html_ddr.html`

A FileMaker HTML Database Design Report of a real solution, used by
`TestConvertHTMLDDR_RecipeDatabase` to check that the HTML DDR reader keeps
resolving a whole solution — base tables, fields, table occurrences including a
self-join, relationships, layouts and accounts — rather than only the shapes our
own hand-written fixtures happen to cover.

Recorded as required by
[`docs/LEGAL.md`](../../../../docs/LEGAL.md#reference-and-asset-provenance), and
summarised in the ledger at
[`docs/PROVENANCE.md`](../../../../docs/PROVENANCE.md).

| Item | Record |
| --- | --- |
| File and author | `recipes_html_ddr.html`, by Peter Hebert. Copied verbatim from `documentation/recipes_ddr/recipes.html` in `peterhebert/FileMaker-recipe-database`, renamed only to say what it is. `sha256:f5ecd82b039cdc3ff262967c879485253c05a62f681a893a10ebba7f3d70b0a2`, 125941 bytes. |
| External reference | *FileMaker recipe database* — "A basic recipe database for use on an iPad with FileMaker Go". `https://github.com/peterhebert/FileMaker-recipe-database`, commit `350d3bcc0d9b64d96769a4925d9c6b758aea3a21` (2014-04-04). Retrieved 2026-10-26. The report itself is output of FileMaker's own Database Design Report feature, run by that project's author on their own solution. |
| Purpose | Example data: an input document for the HTML DDR reader. It is read by tests only — never served, rendered, or shipped in a release archive, and none of its text or artwork appears in File4Base's own prose or UI. |
| Rights | The Unlicense: "Anyone is free to copy, modify, publish, use, compile, sell, or distribute this software, either in source code form or as a compiled binary, for any purpose, commercial or non-commercial, and by any means." A public domain dedication, so redistribution inside this repository is permitted outright. No notice is required; this record is kept as evidence practice, and the upstream `LICENSE` is quoted above rather than vendored because the dedication imposes no condition to carry. No unresolved questions. |
| Changes | None. The file is byte-for-byte upstream; the hash above is of the file as committed. Its sibling `Styles.css`, screenshots, relationship diagram and the binary `recipes.fmp12` were deliberately **not** copied: the stylesheet and diagram are not read by any test, and the 900 KB binary is covered by the note below. |
| AI assistance | Fixture located and this record drafted with Claude Opus 5 (Claude Code), 2026-10-26, from a GitHub code search for FileMaker design reports; the upstream `LICENSE` text and commit were fetched and read rather than inferred from GitHub's license label. Reviewed by the maintainer in the pull request that added this file. |
| Review | Maintainer, 2026-10-26: added as a test fixture. This changes the earlier disposition in `docs/PROVENANCE.md`, which cited the sample by URL and stored nothing; the change is recorded there too. |

### The binary `recipes.fmp12` is still not stored here

The Unlicense permits it, but `server/internal/migration/fmp12_records.go` has no
fixture of a real `.fmp12`, so its record and container extraction are exercised
only against synthesised HBAM pages. Adding the upstream 900 KB binary would
cover that path — against a file that really does carry three JPEG container
images — at the cost of carrying it in the repository forever. That trade-off is
a maintainer decision and has not been made; until it is, reproduce it locally:

```bash
curl -sSLO https://raw.githubusercontent.com/peterhebert/FileMaker-recipe-database/350d3bcc0d9b64d96769a4925d9c6b758aea3a21/recipes.fmp12
```
