# Licensing, independent development and reference material

File4Base is an independent project. References to Claris products identify
those products and do not imply affiliation, sponsorship or endorsement.
Claris, FileMaker and FileMaker WebDirect are trademarks of Claris International
Inc. The File4Base browser component is called **File4Base Web Client**.

This document sets contribution and release-review practices. It does not
certify non-infringement or resolve historical provenance. The findings and
remaining clearance work are tracked in [audit issue #44](https://github.com/file4base/file4base-app/issues/44).

## Project license and third-party material

The project release metadata identifies **GPL-3.0-only**. See the full
[project license](../LICENSE). Third-party components retain their applicable
licenses and notices; this document does not relicense them or supply rights
in material that a contributor was not entitled to contribute.

For each distributed archive/image, maintainers should verify:

- The project license and applicable third-party copyright, license and notice
  material accompany the distribution in an accessible location.
- The recipient can identify the exact corresponding source and build scripts
  for that version. GitHub's matching tag source archives are useful; source
  directions for separately hosted images must also be clear.
- Dependencies downloaded during native builds are covered, as well as Go/Dart
  package dependencies. Inspect the final archive/image, not only the source
  tree or the build stage.

These checks support the applicable distribution conditions, including
[GPLv3 sections 4–6](https://www.gnu.org/licenses/gpl-3.0.html), and each
dependency's own license. The existing notice work in issues
[#7](https://github.com/file4base/file4base-app/issues/7) and
[#22](https://github.com/file4base/file4base-app/issues/22) does not establish
that every native dependency is covered; PDFium is tracked in
[#43](https://github.com/file4base/file4base-app/issues/43).

## Functional compatibility and original presentation

Describe requirements in terms of user tasks, input/output behavior and
necessary compatibility. Use File4Base's own presentation, artwork, explanations
and examples. Common functional commands and justified compatibility identifiers
can be retained. Do not set pixel reproduction of a proprietary interface as
the acceptance criterion for a feature.

The legal distinction is explained by [CJEU C-406/10, SAS Institute](https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=celex%3A62010CJ0406)
on functionality and manual expression, and [C-393/09, BSA](https://eur-lex.europa.eu/legal-content/en/ALL/?uri=celex%3A62009CJ0393)
on original GUI expression. Independently written code does not by itself clear
an interface, manual, image or sample dataset. The current comparison and
tutorial work remain open in [#40](https://github.com/file4base/file4base-app/issues/40)
and [#41](https://github.com/file4base/file4base-app/issues/41).

This policy deliberately reduces unnecessary expressive similarity while
preserving useful behavior. It does not assert that every shared label,
shortcut, colour or database concept is protected, or that changing a fixed
percentage of a screen provides legal clearance.

## Reference and asset provenance

Record the following in a contribution or its linked provenance record:

| Item | Information to record |
| --- | --- |
| Files and author | Affected paths, author/rightsholder and origin |
| External references | Actual title, product/version, URL and date; what was consulted |
| Purpose | Functional observation, visual reference, text, artwork or example data |
| Rights | Applicable license/permission, notices and distribution scope; unresolved questions |
| Changes | Independently created material, adapted material and changes made |
| AI assistance | Tool and relevant reference inputs where known; human review performed |
| Review | Reviewer, date and decision or outstanding issue |

Do not invent missing provenance, replace vendor names inside historical source
citations, or describe independent coding as a clean-room process unless such a
process actually occurred. Preserve development history and evidence. Keep
confidential agreements, prompts and licensed source material in an appropriate
private location; record a safe reference rather than publishing their contents.

For observed proprietary software, establish the lawful access basis and actual
accepted agreement. [Directive 2009/24/EC, articles 5(3), 6 and 8](https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=CELEX%3A32009L0024)
distinguishes observation from limited interoperability decompilation and
protects specified statutory exceptions. Public documentation or a normal
software license should not be assumed to permit redistribution of manuals,
screenshots, sample files or artwork. Resolve uncertain permissions before
adding such material.

The ledger is an evidence practice, not a claim that legislation universally
requires this form. Completing historical records is tracked separately in
[#42](https://github.com/file4base/file4base-app/issues/42).

## Product names and comparisons

Use independent File4Base names for our components. Use Claris product names
only to identify those products in accurate comparisons or interoperability
documentation, with appropriate attribution. Do not imply certification or
endorsement. State the specific tested behavior and version when making a
compatibility claim.

[EU Trade Mark Regulation 2017/1001, article 14](https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=CELEX%3A32017R1001)
addresses honest referential use. [Claris's trademark guidelines](https://www.claris.com/company/legal/trademark-guidelines)
are also relevant to its names, but are the proprietor's policy rather than
legislation. Independent naming reduces an avoidable association risk; an
attribution notice does not cure otherwise confusing presentation. Remaining
application labels and name clearance are tracked in
[#8](https://github.com/file4base/file4base-app/issues/8).

## Rights questions and notices

Flag unresolved rights questions for a maintainer before inclusion. A public
issue can identify the affected File4Base paths and requested review without
uploading proprietary reference material or personal information. Maintainers
should arrange a private channel where sensitive evidence is necessary.

On receiving a rights notice, preserve the notice and relevant evidence and
obtain advice before responding or submitting a counter-notice. GitHub's
[copyright](https://docs.github.com/en/site-policy/content-removal-policies/dmca-takedown-policy)
and [trademark](https://docs.github.com/en/site-policy/content-removal-policies/github-trademark-policy)
procedures are distinct. This policy is not immunity from either process.
