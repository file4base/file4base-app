# The design report

What a solution is made of, written out: **Tools > Database Design Report...**
and **Tools > Save Design as XML...** (#49).

It answers two needs that are usually built twice:

- a **report to read**, for documenting a solution, reviewing it before it is
  handed over, or explaining it to someone who did not build it;
- the **design as text**, so that a solution can live in version control. A
  `.f4p` file is MessagePack: exact, compact and completely opaque to
  `git diff`. The same design as XML or JSON is not.

Both come from **one walk of the catalog** — the same one the solution export
uses — so what the report says and what the text form holds cannot drift
apart, and neither can drift from the `.f4p` file.

## What it describes

| Section | Contents |
| --- | --- |
| Tables | Every table and field: display name, column name, type, primary key, whether the column accepts nothing, the calculation formula, the field options and the validation rules, as they are stored. |
| Relationships | Each join read left to right as parent to child: the two occurrences and match fields, the operator, and whether records can be created through it, deletes cascade, or related records are sorted. The table occurrences are listed with the tables they stand for. |
| Layouts | The occurrence a layout is on, its parts, how many objects of each kind it holds, the fields it binds — with the relationship when the field is a related one, and the value list when it has one — its portals, and the scripts it runs from a button or a trigger. |
| Scripts | Each script with its context, whether it is active, and every step in order with its parameters. A disabled step is struck through rather than left out. |
| Value lists | Custom lists with their values, and field lists with the field they read. |
| Accounts | Each account with its role, whether it is active, and its layout permissions. |
| Privileges | The extended privileges each role holds (#39). |
| Saved finds | Each saved find with the table it searches and the criteria of every request, as they were typed (#34). |
| Data sources | Each registered external connection: engine, host, database, user and the environment variable a password is read from — never a password (#47). |

## What it never contains

- **No records.** The report describes the structure and counts no data.
- **No password, password hash or stored credential**, in any format. An
  account appears with its name, role and layout permissions only. A report is
  something you can attach to an email.
- **No client settings** kept in the solution file (File Options, Page Setup).

The report says all of this about itself, in a closing section, so that what
is absent from it is not read as absent from the solution.

## The three forms

| Format | What it is |
| --- | --- |
| `html` | One self-contained page: no script, no font to fetch, prints as it reads. It shows the moment it was made. |
| `xml` | The same design, parseable. Each stored document — a field's options, a step's parameters, a layout's definition — travels as its canonical JSON text, because there is no faithful element form of an arbitrary object and inventing one would make the report lie about what is stored. |
| `json` | The same design with those documents as the objects they are, keys in order. |

## Why the text forms have no timestamp

A design written twice without changing the solution must produce **the same
bytes**, or every commit would show a diff that means nothing. So the XML and
JSON forms carry no `generated_at`, and every collection is written in the
catalog's own order — tables by display name, fields in the order they were
added, scripts by name, steps by sequence, and anything a map would have left
unordered sorted by name. The HTML report, which nobody commits, shows the
time.

The text forms also carry **each layout's definition exactly as it is stored**,
so a change to a layout is a readable diff rather than an invisible one.

## The endpoint

`GET /api/v1/solutions/design-report?format=html|xml|json&solution=<name>`,
owner or admin. See
[docs/api/API_REFERENCE.md](../api/API_REFERENCE.md).

## What is not here yet

- **Nothing is read back.** The report is written, not imported; restoring a
  solution is still the `.f4p` file. Making the text form importable would
  make it a second bundle format, with everything that implies.
- **A layout is described, not drawn.** There is no picture of a layout in the
  report.
- **No comparison between two reports.** `git diff` is the comparison; File4Base
  does not have one of its own.
