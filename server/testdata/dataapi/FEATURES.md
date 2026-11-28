# F4B_DataAPI_Reference: what the reference file contains

`F4B_DataAPI_Reference.fmp12` is a synthetic FileMaker file written for the
Data API conformance suite (#208). Every name and value in it is made up. It
exists so that each behaviour of the Data API can be recorded against a known
object. The short ids in brackets are the audit items of the #208 gap audit
that the object serves; the fixtures carry the same ids in their `rows`.

## How it is built

1. `gen_reference.py` writes a FileMaker Save-as-XML document (format 2.3.0.0)
   for the whole schema, and `expected-data.json` with every literal value.
2. FileMaker's `FMUpgradeTool --generateDBFile` compiles that XML into an
   `.fmp12`. The build is accepted only when it reports no failed node, and
   when FileMaker's own Save-as-XML of the result matches the source. UUIDs,
   hashes, recomputed step option bits and theme formatting are ignored.
3. The compiled file has no records. Running its `Build Reference Data`
   script once in FileMaker Pro, single user, creates them. It also sets the
   global values, which persist only when set in single-user mode.
4. The result is `reference/F4B_DataAPI_Reference.fmp12`, with blank account
   passwords. `reference/F4B_DataAPI_Reference.xml.gz` is FileMaker's
   Save-as-XML of that same data copy; the conformance test migrates both.

## Schema

| Table | Fields | Records |
|---|---|---|
| Customers | 49 | 30 |
| Orders | 16 | 60, for 24 customers |
| OrderLines | 11 | 120 |
| Contacts | 11 | 30 |
| Products | 5 | 30 |

Record ids are 1..n, in creation order. Six customers have no order, so the
empty-portal cases exist.

- **Field types** (M7, FD6–FD10, FD14): text, number, date, time, timestamp and
  container, each also as a global (`gText`, `gNumber`, `gDate`, `gTime`,
  `gTimestamp`, `gContainer`). `gRepeat` is a global with 3 repetitions and
  `gReport` a global text that scripts write to.
- **Calculations** (M5, FD15): stored (`cDisplayName`, `cBalanceRounded`,
  `cSignupPlus30`, `cDurationPlusHour`, `cContactStamp`, `LineTotal`,
  `cFullName`) and unstored (`cNameUpper`, the container `cPhotoCopy`, the
  related aggregates `cOrderCount` and `cOrdersTotal`, `cCustomerName`,
  `cDivByZero`, the repeating `cRepeatCalc`, `cGlobalEcho`).
- **Summaries** (M5, FD16): counts, totals and an average.
- **Auto-enter** (M9, C19): serials on every `*ID` key, creation and
  modification timestamps and account names, and a constant.
- **Validation** (M10, C14): not empty, unique, range with strict numeric,
  maximum length, four-digit year, a calculation with a custom message, and
  membership of a value list.
- **Repetitions** (M11, FD4, C5, GL6): `Tags` (text, 3, all shown),
  `Scores` (number, 4, 2 shown), `cRepeatCalc` and `gRepeat`.

## Values chosen to break things

- **Numbers** (FD6, FD7, C7): pi, `-1.23e-19`, a 30-digit integer, ±(2^53+1),
  `0.30000000000000004`, `007`, empty values; and a number field holding text
  such as `abc`, `12abc`, `1,5`, `$1,234.56`, `50%`, `0x1F`, Arabic-Indic
  digits, `½`, `NaN`, `1 000 000`, `12:30` and `2024-01-01`.
- **Dates** (FD8, F14): from 0001-01-01 to 4000-12-31, including 1066, the
  1582 calendar switch, 1899-12-30, 1900-02-28, 1904-01-01, leap days and
  2038-01-19/20.
- **Times** (FD9, FD11): durations of 25 h, 100 h and 1,000,000 h, negative
  durations, fractions down to the millisecond, and timestamps with fractions
  from year 0001 to 4000.
- **Text** (FD18): carriage returns, tabs, quotes and backslashes, Latin-ext,
  Greek, Cyrillic, Arabic, Hebrew, CJK and emoji, leading zeros, long notes
  and empty values.
- **Containers** (FD14, CU1): a PNG, a GIF and a text file in `Photo`, and a
  PNG in `gContainer`.

## Relationships, value lists, layouts and scripts

- **Table occurrences** (FD2, FD3, PD1–PD5, DL3): 15, with two child tables of
  Customers, a two-hop chain, a self-join on Country, reverse lookups, "allow
  creation" on three relationships, cascade delete from Orders to OrderLines,
  and a sorted relationship.
- **Value lists** (M12, M14–M16, R6): six custom lists (one with accented and
  non-Latin values) and three field-based ones: both fields, second field
  only, and related values.
- **Layouts** (LN1–LN6, M2–M6, M13, R14): one base layout per table, a
  separator at the top level and inside a folder, and an `API` folder with:
  - `API_Customers`: every display type, a field placed twice, repetitions,
    related fields outside a portal;
  - `API_Customers_Portals`: five portals (sorted, filtered, unnamed, two-hop,
    self-join) and a field placed off the visible area;
  - `API_Customers_Restricted`, with a field the read/write account cannot
    see; `API_NoAccess`, which neither API account can open; `API_Empty`, with
    no field; two layouts named `API_Duplicate`; a nested folder holding
    `API_Customers_Nested`; and layouts for the other tables.
- **Scripts** (SN1–SN4, SC1–SC5, R19–R22, F27, GL4): an `API` folder with
  scripts that succeed, echo their parameter, fail with 401 or 3, change the
  found set, sort, set a global, set and read a `$$` variable, return Unicode,
  and report their context; a separator and a nested folder. A `Setup` folder
  holds `Build Reference Data` and a `Self Check` report.

## Accounts (AC1, AC4, X17, L5, LN6, SN3, M18, R23, EC3, EC8)

| Account | Privilege set | Data API access |
|---|---|---|
| admin | [Full Access] | yes |
| api_rw | DataAPI_ReadWrite | yes; one field and one layout without access; layouts read-only, records read/write |
| api_ro | DataAPI_ReadOnly | yes; view only; one layout without access |
| noapi | NoAPI_DataEntry | no (401, code 9) |

`[Guest]` exists, disabled. The file ships with blank passwords: whoever hosts
it sets their own, and the conformance test sets its own after the migration.

## What the generator cannot express

- FileMaker's XML compiler does not link control-flow steps: an `If` whose
  condition is false, or a `Loop`, never reaches its end and the script hangs.
  The reference scripts are therefore straight-line, and collect errors with
  the `If()` function.
- Layout folder end markers are not accepted, so each folder ends with the
  list: `API` is the last top-level folder and `Nested` the last item in it.
- Not included: tab, slide and popover objects; value lists or occurrences
  from another file; concealed edit boxes; external authentication;
  containers stored by reference or externally.
