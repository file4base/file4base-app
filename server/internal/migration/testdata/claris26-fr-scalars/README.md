# Literal source observations

An original synthetic expression was evaluated in the Data Viewer of FileMaker
Pro 26.0.2 with French system settings on 2026-11-13. No database was open:
`Get(FileName)` returned empty. `source-results.json` contains the actual result;
all values are recorded as strings to preserve decimal and time formatting.
The expression and input strings were independently written for File4Base
compatibility work with Codex assistance. No vendor example, customer record,
private script, screenshot, license or credential is included. These original
test artifacts are contributed under the repository's GPL-3.0-only terms.

This checks scalar function behavior only. It does not qualify Number field
storage, FMPXMLRESULT export formatting, source file locale, cross-file workflows,
containers, or the two-file application acceptance fixture. A source conversion
such as GetAsNumber("1/2") returning "12" must not be confused with preserving
that original text, and does not justify replacing permissive source Number
values with a guessed fraction. French comma decimals and durations also require
explicit compatibility work rather than inference from a generic import parser.
