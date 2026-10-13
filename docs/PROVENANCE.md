# File4Base Reference, Asset, and Design Provenance Ledger

This ledger records the external reference materials consulted during the
design of **File4Base**, the complete inventory and origin of shipped artwork
and mockups, the UI presentation review and design tokens adopted under
[Issue #40](https://github.com/file4base/file4base-app/issues/40), the tutorial
remediation completed under
[Issue #41](https://github.com/file4base/file4base-app/issues/41), the source
citation and provenance records required by
[Issue #42](https://github.com/file4base/file4base-app/issues/42), and the
disposition of the master IP audit tracker in
[Issue #44](https://github.com/file4base/file4base-app/issues/44).

File4Base is an independent open-source project (`GPL-3.0-only`). References to
Claris International Inc. products (`FileMaker`, `FileMaker Pro`, `FileMaker
WebDirect`) identify those products for historical citation and functional
interoperability context only and do not imply affiliation, sponsorship, or
endorsement.

---

## 1. External Reference Material Ledger (`#42`)

During initial specification (`9c233688`, `d1538c1d`, September–October 2026),
an automated string substitution in the markdown specifications replaced
occurrences of `FileMaker Pro` with `file4base Pro` / `File4base Pro` inside
reference citations. Those citations have been restored to their actual external
titles in [`docs/specs/FUNCTIONAL_SPECIFICATION.md`](specs/FUNCTIONAL_SPECIFICATION.md),
[`docs/specs/file4base_menu_reference_guide.md`](specs/file4base_menu_reference_guide.md),
and [`docs/specs/DBAL_SPECIFICATION.md`](specs/DBAL_SPECIFICATION.md).

| Reference Title & Version | Publisher / URL | Consulted For | Affected File4Base Paths | Current Disposition & Remediation |
| --- | --- | --- | --- | --- |
| *FileMaker Pro 14 User's Guide* and *FileMaker Pro 12–19 Help* | Claris International Inc. (`https://help.claris.com/archive/help/`) | Functional behavior of the 4 operational modes (`Browse`, `Find`, `Layout`, `Preview`), calculation operators/functions, `FMPXMLRESULT` export grammar, and high-level menu categories | [`docs/specs/FUNCTIONAL_SPECIFICATION.md`](specs/FUNCTIONAL_SPECIFICATION.md), [`docs/specs/file4base_menu_reference_guide.md`](specs/file4base_menu_reference_guide.md), [`docs/specs/calculation_formulas.md`](specs/calculation_formulas.md), [`docs/specs/importing_and_exporting_records.md`](specs/importing_and_exporting_records.md) | Citations restored accurately (`#42`). Functional behavior is independently implemented in Go (`server/`) and Flutter (`client/`) with File4Base's own prose and schemas. |
| *FileMaker Pro 12 Tutorial / Help Sample* (*Favorite Bakery* / `ABC Company` / `XYZ Inc.`) | Claris International Inc. (`https://help.claris.com/archive/help/12/fmp/en/html/create_chart.12.16.html`) | Historical lesson sequence and sample business scenario in the retired `docs/tutorials/favorite_bakery.md` | [`docs/tutorials/seed_archive.md`](tutorials/seed_archive.md), [`docs/tutorials/favorite_bakery.md`](tutorials/favorite_bakery.md), test fixtures in `server/` and `client/test/` | **Retired and replaced (`#41`).** Replaced by the independent *Boreal Seed & Herbarium Archive* scenario (`seed_archive.md`) and updated test fixtures. |
| User-supplied Spanish relational graph reference observation (`CLIENTES` / `FACTURAS`) | Maintainer functional workflow observation (September 2026; no external screenshot shipped in repository) | Observed table-occurrence node linking and selection state in `RelationshipGraphWidget` | [`client/lib/features/schema_manager/widgets/relationship_graph_widget.dart`](../client/lib/features/schema_manager/widgets/relationship_graph_widget.dart) | **Redesigned (`#40`).** Replaced the amber/gold halo styling with File4Base's Slate & Sky-Cyan (`#0284C7` / `#38BDF8`) card header, pill badge, and focus ring. |
| PDFium native binary (`libpdfium.so` / `pdfium.dll`, chromium/5200) | PDFium Authors via `bblanchon/pdfium-binaries` (bundled by Dart `printing 5.15.1` plugin) | PDF rendering and print preview on Linux and Windows desktop builds | [`THIRD_PARTY_NOTICES/pdfium/LICENSE`](../THIRD_PARTY_NOTICES/pdfium/LICENSE), [`THIRD_PARTY_NOTICES/pdfium/README.md`](../THIRD_PARTY_NOTICES/pdfium/README.md), `scripts/build_desktop.sh`, `scripts/verify_desktop_archive.sh` | **Resolved (`#43`).** Upstream BSD-3-Clause `LICENSE` and `VERSION` are installed by CMake into Linux/Windows release archives and verified in CI. |
| `FileMaker-recipe-database` (`recipes.fmp12` & `recipes.html` DDR) | Peter Hebert (`https://github.com/peterhebert/FileMaker-recipe-database`) | End-to-end interoperability validation of HTML DDR + `.fmp12` migration (portals, self-join disambiguation, `CONTAINER` images, `Group Buttons`, and scripts) | [`server/internal/migration/html_ddr.go`](../server/internal/migration/html_ddr.go), [`server/internal/migration/fmp12_records.go`](../server/internal/migration/fmp12_records.go), [`docs/tutorials/filemaker_migration_guide.md`](tutorials/filemaker_migration_guide.md) | External sample cited by URL in [`docs/tutorials/filemaker_migration_guide.md`](tutorials/filemaker_migration_guide.md); no third-party screenshots or binary `.fmp12` files are stored in the repository. |

---

## 2. Shipped Artwork, Branding, and Mockup Inventory (`#42`)

No Claris icons, logos, bitmaps, or screenshots are stored in or distributed
from this repository. All branding and wireframe files in `assets/` and
`client/assets/` are original File4Base creations:

| Asset Path(s) | Kind | Author / Rightsholder | Origin Commit & Date | Provenance & Creation Method | License |
| --- | --- | --- | --- | --- | --- |
| `assets/branding/file4base-dark.svg`, `client/assets/branding/file4base-dark.svg` | Vector master (dark squircle `#131d2e` + cyan `#38bdf8` folder/numeral-4/database cylinder glyph) | Mario Ezquerro (`@file4base`) | `9c233688` (2026-09-22) / `ccb9af80` (2026-09-23) | Hand-authored 29-line SVG vector path artwork created specifically for File4Base | `GPL-3.0-only` |
| `assets/branding/file4base-light.svg`, `client/assets/branding/file4base-light.svg` | Vector master (light variant of the File4Base squircle icon) | Mario Ezquerro (`@file4base`) | `9c233688` (2026-09-22) / `ccb9af80` (2026-09-23) | Hand-authored 50-line SVG vector path artwork derived from `file4base-dark.svg` | `GPL-3.0-only` |
| `assets/branding/file4base-icon*.{png,jpg}`, `assets/branding/file4base-{dark,light}.{png,ico}`, and matching copies in `client/assets/branding/`, `client/web/icons/`, `client/macos/Runner/Assets.xcassets/`, `client/windows/runner/resources/app_icon.ico` | Rasterized application icons (`64px`–`1024px`, `.ico`) | Mario Ezquerro (`@file4base`) | `54c7c244` (2026-09-23) | Direct raster exports generated from `file4base-dark.svg` and `file4base-light.svg` | `GPL-3.0-only` |
| `assets/mockups/script_workspace_mockup.png` | Internal design wireframe (`1600×900` dark-mode mockup titled `File4Base - Script Workspace`) | Mario Ezquerro (`@file4base`) | `1f092d44` (2026-09-25) | Original File4Base UI concept wireframe rendered during Phase 6 planning (`docs/specs/script_workspace_spec.md`); not a screenshot of any third-party product | `GPL-3.0-only` |
| `assets/mockups/filemaker_migration_pipeline.svg`, `assets/mockups/filemaker_migration_layouts.svg`, `assets/mockups/file4base_edit_recipe_portal_error.png`, `assets/mockups/file4base_edit_recipe_resolved.png` | Original SVG architecture/layout diagrams and File4Base Web Client screenshots for [`docs/tutorials/filemaker_migration_guide.md`](tutorials/filemaker_migration_guide.md) | Mario Ezquerro (`@file4base`) | 2026-10-13 | Hand-authored SVG diagrams and captures of File4Base Web Client rendering the migrated `Edit Recipe` layout | `GPL-3.0-only` |
| Runtime UI icons across `client/lib/` | Material Design Icons (`Icons.*`) | Google / Flutter SDK (`uses-material-design: true`) | Standard Flutter SDK dependency | Covered by Flutter's bundled `NOTICES.Z` / Material Icons Apache-2.0 license | `Apache-2.0` |

---

## 3. UI Presentation Review & File4Base Design Tokens (`#40`)

Under [CJEU C-406/10 (*SAS Institute*)](https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=celex%3A62010CJ0406)
and [CJEU C-393/09 (*BSA*)](https://eur-lex.europa.eu/legal-content/en/ALL/?uri=celex%3A62009CJ0393),
functional database workflows and standard desktop concepts are distinct from
discretionary visual composition. The table below documents the separation
between functional requirements and File4Base's own visual design choices implemented in `#40`:

| Surface | Functional / Technical Requirement | Discretionary Presentation Redesigned for File4Base (`#40`) |
| --- | --- | --- |
| **Brand & Studio Palette** ([`theme_model.dart`](../client/lib/core/theme/theme_model.dart), [`file4base-dark.svg`](../assets/branding/file4base-dark.svg)) | Clear contrast in light and dark modes across desktop and web | File4Base Slate & Sky-Cyan palette: deep slate surfaces (`#0F172A`, `#1E2430`, `#283142`), sky-cyan primary accents (`#0284C7` light / `#38BDF8` dark), and cool slate borders (`#CBD5E1` / `#334155`). |
| **Layout Mode Part Rail** ([`layout_designer_widget.dart`](../client/lib/features/layout_engine/layout_designer_widget.dart)) | Identify vertical layout bands (`Header`, `Sub-summary`, `Body`, `Trailing Grand Summary`, `Footer`), show their heights, and open Part Setup on double-click | Replaced the narrow 32px rotated-text gutter (`RotatedBox(quarterTurns: 3)`) with File4Base's **96px Horizontal Part Rail**: horizontal left-accented cards displaying readable horizontal part titles and live point-height readouts (`60 pt`). |
| **Layout Studio Action Bar** ([`layout_designer_widget.dart`](../client/lib/features/layout_engine/layout_designer_widget.dart)) | Provide a clear control to commit layout edits and return to Browse mode | Styled the `Exit Layout` action button and studio subbar with File4Base's primary sky-cyan (`#0284C7`) flat rounded-6px token and updated [`docs/index.html`](index.html). |
| **Relationships Graph** ([`relationship_graph_widget.dart`](../client/lib/features/schema_manager/widgets/relationship_graph_widget.dart)) | Display table occurrences, columns, primary keys, and cubic-Bezier join lines with operator badges (`=`, `≠`, `<`, `>`) | Replaced screenshot-referenced amber/gold borders and halos (`#FF9800` / `#FFB300`) with File4Base's slate/sky-cyan card header (`#0C4A6E` / `#E0F2FE`), cyan focus ring, `Icons.dataset_linked_outlined` header icon, and rounded pill operator badges (`borderRadius: 11`). |
| **Manage Database Dialog** ([`manage_database_dialog.dart`](../client/lib/features/schema_manager/manage_database_dialog.dart)) | Tabbed modal for managing databases, tables, fields, and relationships with bulk table actions | Uses Material 3 tonal action pills, inline row action buttons (`Fields`, `Copy Structure`, `Rename`, `Duplicate`, `Truncate`, `Delete`), and File4Base's selection summary bar. |

---

## 4. Tutorial Rewrite & Sample Dataset Record (`#41`)

- **Retired material**: `docs/tutorials/favorite_bakery.md` previously used a
  *Favorite Bakery* annual-fee discount narrative and `Customers` / `Companies`
  (`ABC Company`, `DEF Ltd.`, `XYZ Inc.`) that overlapped the Claris FileMaker
  Pro 12 tutorial sample setting.
- **Replacement**: Authored [`docs/tutorials/seed_archive.md`](tutorials/seed_archive.md)
  (*Boreal Seed & Herbarium Archive*), teaching all 21 lessons through a
  botanical seed-repository domain (`Accessions` and `Stations`, `Nordic Vault`,
  `Alpine Lab`, `Coastal Botanical`, `Cryogenic` vs `Standard` preservation
  tiers).
- **Verification**: All 16 accession records, find counts, multi-request/omit
  finds, sort tie-breakers, calculation formulas, summary subtotals (`1080` +
  `750` = `1830`), and portal counts (`6`, `5`, `5`) are verified by unit tests
  in [`client/test/tutorial_walkthrough_test.dart`](../client/test/tutorial_walkthrough_test.dart).
  All remaining test fixtures in `server/` and `client/test/` were updated to
  use the new botanical station fixtures.

---

## 5. AI Assistance & Contributor Review Record (`#42`)

- **AI-assisted development**: Portions of the Go backend, Flutter client, unit
  tests, and technical documentation were developed with assistance from coding
  agents (Claude / Google Antigravity) directed and reviewed by the project
  maintainer (`Mario Ezquerro`).
- **Governance rules enforced**: [`AGENTS.md`](../AGENTS.md),
  [`CONTRIBUTING.md`](../CONTRIBUTING.md), and [`docs/LEGAL.md`](LEGAL.md)
  require that all contributions express requirements in terms of functional
  behavior, use File4Base's own visual presentation and prose, preserve accurate
  source citations, and verify third-party license notices in built artifacts.

---

## 6. Master Audit Tracker Disposition & Ongoing Clearance Governance (`#44`)

| Issue | Scope | Status | Reviewer & Date |
| --- | --- | --- | --- |
| [`#8`](https://github.com/file4base/file4base-app/issues/8) | Independent browser-client naming (**File4Base Web Client**) and trademark attribution | **Completed & Closed** (`5ab6758`) | Mario Ezquerro (2026-10-13) |
| [`#43`](https://github.com/file4base/file4base-app/issues/43) | Bundle upstream PDFium `LICENSE` and `VERSION` in Linux/Windows desktop archives + CI check | **Completed & Closed** (`5ab6758`) | Mario Ezquerro (2026-10-13) |
| [`#41`](https://github.com/file4base/file4base-app/issues/41) | Replace tutorial scenario and test fixtures with independent *Boreal Seed & Herbarium Archive* | **Completed** | Mario Ezquerro (2026-10-13) |
| [`#42`](https://github.com/file4base/file4base-app/issues/42) | Restore accurate specification citations and publish consolidated asset/reference provenance ledger | **Completed** | Mario Ezquerro (2026-10-13) |
| [`#40`](https://github.com/file4base/file4base-app/issues/40) | Redesign discretionary UI chrome (Horizontal Part Rail, Slate/Cyan graph tokens, Exit Layout action) | **Completed** | Mario Ezquerro (2026-10-13) |

### Designated Owner & Ongoing Pre-Commercial Clearance Protocol

- **Designated Rights & Takedown Contact**: Mario Ezquerro (`mario.ezquerro@gmail.com`),
  project maintainer, is designated to receive, preserve, and evaluate any
  copyright, trademark, or platform takedown notices in accordance with
  [`docs/LEGAL.md`](LEGAL.md) before any counter-notice or modification is made.
- **Scope of Current Distribution**: File4Base is distributed solely as an
  open-source, self-hosted software project under `GPL-3.0-only`. Before any
  commercial licensing, hosted SaaS offering, or registered trademark filing in
  the EU (including Spain and France) or other jurisdictions, the maintainer
  requires formal register searches (EUIPO/OEPM/INPI/WIPO marks, EU registered
  designs, and jurisdiction-specific patent/counsel review) using the records in
  this ledger.
