# FileMaker Data API conformance fixtures

This directory is the reference side of the Data API conformance suite of
#208. It holds what FileMaker Server answered, and
`server/internal/api/dataapi_conformance_test.go` checks File4Base against
it.

| Path | What it is |
|---|---|
| `reference/F4B_DataAPI_Reference.fmp12` | The synthetic reference file, with its records, blank passwords |
| `reference/F4B_DataAPI_Reference.xml.gz` | FileMaker's Save-as-XML of that file, gzipped |
| `gen_reference.py` | The generator of the file's schema and data (see `FEATURES.md`) |
| `FEATURES.md` | What the file contains, and why |
| `fixtures/<group>/<scenario>@<version>.json` | One recorded scenario, for one API version |
| `expected_failures.txt` | Where each fixture File4Base does not pass yet first fails |

Everything here was produced by the File4Base project: the file, its data and
the scenarios are ours, and the fixtures are FileMaker Server's answers to
them.

## What the test does

1. It migrates the reference file through `POST /api/v1/solutions/migrate/apply`,
   sending the native file and its Save-as-XML design, into a database named
   `F4B_DataAPI_Reference`, and gives the accounts `admin`, `api_rw`, `api_ro`
   and `noapi` test passwords.
2. It replays every step of every fixture against `/fmi/data/...`, in order,
   on host `fms.conformance.test` over TLS, and compares the status, the
   recorded headers and the body.
3. Fixtures marked `"fresh": true` write data; each gets its own migration.
   The others share one migration, and never change persistent data.

Run it locally against a disposable database server:

```sh
cd server
F4B_TEST_ENGINE=postgres \
F4B_TEST_ADMIN_DSN='postgres://file4base:dev_password@localhost:5432/postgres?sslmode=disable' \
F4B_TEST_DEV_DSN='postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable' \
go test -count=1 -run '^TestDataAPIConformance' -v ./internal/api/
```

For MariaDB, set `F4B_TEST_ENGINE=mariadb` and MariaDB DSNs; the account needs
`GRANT ALL PRIVILEGES ON *.* ... WITH GRANT OPTION`, as in CI.

## Exact comparison, and the closed list of differences

- **Status:** equal.
- **Length:** a declared `Content-Length` must be the length of the body, and
  a `1xx`, `204` or `304` answer must have none.
- **Headers:** `Content-Type`, `Cache-Control`, `Set-Cookie`,
  `Content-Disposition`, `Location`, `WWW-Authenticate` and every `X-FM-*`
  header. The same names, with the same values, and no extra one. Names are
  compared, and read for captures, case-insensitively, as a client does.
- **JSON bodies** are compared token by token: key order, duplicate keys, value
  types and the spelling of numbers (`-3.0` is not `-3`) all count.
  Whitespace and string escaping do not. Keys are always compared literally;
  placeholders apply to values only.
- **Other bodies** are compared as text, or byte for byte for binary content
  (container downloads).

The only tolerated differences are the placeholders the recorder writes in
place of values that cannot be reproduced. The list is closed: adding to it
is a reviewed change to the test and to this file.

| Placeholder | Stands for | Must be |
|---|---|---|
| `<token>` | A session token, in a body or a header | An RFC 6750 `b64token` |
| `<cookie>` | The value of a cookie the server sets | RFC 6265 cookie octets |
| `<host>` | The host of a container URL | Exactly the replay host, `fms.conformance.test` |
| `<container-token>` | The path token of a container URL; the downloaded bytes are still compared | `[A-Za-z0-9._~%-]+` |
| `<now>` | A timestamp that depends on the current time | A FileMaker timestamp within 36 hours of the replay clock |
| `<date>` | The expiry date of a cookie | A valid RFC 9110 IMF-fixdate |

No grammar admits `<` or `>`, so a placeholder never matches itself. A URL the
server returns is only followed (`request.url`) if it is an `https` URL on the
replay host, with no user information.

Product identity (what `productInfo` and the messages say) and the deliberate
security differences of #208 are not on the list yet. Each will be added with
its own fixture once decided.

## expected_failures.txt

Each line records where a fixture that does not pass yet **first** fails:

```text
FIXTURE STEP KIND [ENGINE]
records/range_paging@v1 login-rw stand-in
routing/envelope_and_routes@v2 version-v3 status
```

`KIND` is the first check of that step that differs, in the order the checks
run: `unreplayable` (an idle step), `stand-in`, `url`, `timeout`, `status`,
`content-length`, `header:NAME`, `token`, `body`. In a `concurrent` step they
are prefixed `background.` or `probe.`, and `no-probe` means the background
request finished before any probe was sent. The optional `ENGINE`
(`postgres` or `mariadb`) restricts a line to one engine, and wins over a line
for every engine.

- An unlisted fixture must pass. A listed one must fail exactly as listed.
- Failing at an earlier step or check, or differently at the same one, fails
  the test as a regression. Failing later fails it too, with the line to put
  instead, so that progress is recorded and cannot be lost again.
- A listed fixture that passes fails the test: remove its line.
- A line naming an unknown fixture, step or kind fails the test.
- A fixture or replay defect (a step without a request or response, a request
  that cannot be built) fails the test whatever the list says.

`migration@ENGINE ISSUE` records that the reference file does not migrate on
`ENGINE` for the known cause tracked by `ISSUE`, which the test recognises by
its error; any other migration failure fails the test. No fixture is replayed
on that engine until the line is removed, and the line must be removed as
soon as the migration works. Fixture lines are only evaluated on engines
where the file migrates.

CI also checks each pull request against its base, and each push to `main`
against the previous commit (`TestDataAPIExpectedFailuresOnlyShrink`). A
fixture may be newly listed, or listed as failing elsewhere, only in the
change that adds it or re-records its exchanges; a new title, note or audit row
is not a re-recording. A line may move to a later step or check. A
`migration@` line may never be added or changed, and a fixture may never be
deleted.

Accounts of a Save-as-XML design are not migrated yet (#213). Until they are,
the test creates a plain user for each missing account, so that the other
differences still show, and every step that signs in with one fails as
`stand-in`.

## Known limitations

These steps are replayed but cannot pass yet, whatever File4Base answers;
they land with the session clock and record locking of phase P1:

- `idle_seconds` steps wait out FileMaker's session timeout. Replaying them
  needs a session clock the test can move (`unreplayable`).
- `find/operators_number_date`, step `date-today`, searches for `//` (today).
  It was recorded on 2026-11-26, the signup date of one customer, and needs a
  clock frozen to that day.
- `concurrent` steps send a background request (a large container upload that
  holds the record open) and repeat a probe until it answers `until_code`.
  Without a way to know when File4Base has locked the record, the probe is
  timed, not synchronised; a stuck probe is not bounded, and a background
  request that times out is not cancelled before the next fixture.
- `<now>` accepts any FileMaker timestamp format close to the replay clock,
  not specifically the format the request asked for, and the clock is not in
  the server's zone.

## Fixture format

```json
{
  "fixture": "sessions/token_lifecycle@v1",
  "scenario": "sessions/token_lifecycle",
  "group": "sessions",
  "version": "v1",
  "fresh": false,
  "title": "...",
  "rows": ["V1", "V2"],
  "recorded": {"server": "FileMaker Server 26.0.2", "date": "2026-11-26"},
  "steps": [
    {
      "id": "login",
      "rows": [],
      "request": {"method": "POST", "path": "/fmi/data/v1/databases/F4B_DataAPI_Reference/sessions",
                  "headers": [["Content-Type", "application/json"], ["Authorization", "{{basic:api_rw}}"]],
                  "body": "{}"},
      "capture": [{"var": "rw", "from": "token"}],
      "response": {"status": 200, "headers": [["X-FM-Data-Access-Token", "<token>"]], "body": {}}
    }
  ]
}
```

- `rows` are the audit item ids of #208 that the scenario or step pins down.
- `request.body` is the exact text sent. `request.multipart` describes the
  parts of an upload (`text`, `base64`, or `repeat` for large bodies).
  `request.url` replaces `path` when a step follows a URL the server returned,
  and `request.cookies` sends the cookies set earlier in the scenario.
- `capture` keeps a value for later steps: the session token (`from: token`),
  a body value (`from: body`, with a `path`) or a header.
- The response holds `body` (JSON, kept as FileMaker wrote it), `body_text`,
  or `body_base64` with `body_sha256` and `body_length`.
- Request placeholders: `{{basic:ACCOUNT}}` (also `ACCOUNT:wrong`,
  `ACCOUNT:empty`, `ACCOUNT:upper` and `raw:USER:PASS`), `{{user:ACCOUNT}}`,
  `{{password:ACCOUNT}}`, `{{token:NAME}}` and `{{var:NAME}}`. An unknown
  token or variable is sent as `no-such-token-NAME` or `missing-var-NAME`,
  both on recording and on replay.

## Re-recording

Recording needs a licensed FileMaker Server, so it is a maintainer task, not a
CI one. The fixtures are only ever regenerated as a whole, from scenarios
that are kept with the recorder.

1. **Isolate the lab.** Use a disposable machine or container that hosts only
   FileMaker Server and this file, with no network interface besides loopback,
   reached only through its hypervisor's console or exec channel. Nothing in
   it may hold real data or credentials.
2. **Prepare a pristine copy.** Set lab-only passwords on the four accounts of
   `reference/F4B_DataAPI_Reference.fmp12` (for example with
   `FMDeveloperTool --changeAccountPassword`) and keep that copy aside. The
   passwords stay in the lab: fixtures only ever contain `{{basic:...}}`.
3. **Run the scenarios inside the lab**, against `https://127.0.0.1`, for
   `v1`, `v2` and `vLatest`. Before every fresh scenario, and once before the
   others, stop FileMaker Server, put the pristine copy back and wait until a
   login succeeds. This makes each recording deterministic.
4. **Normalise only what cannot be reproduced**, with the placeholders above:
   tokens and cookies (also wherever a token appears later, such as a logout
   path), container URL hosts and path tokens, timestamps within a few minutes
   of the request, and cookie expiry dates. Nothing else is touched: no
   re-encoding, re-ordering or re-typing of FileMaker's JSON.
5. **Review before committing:** no password, token, cookie, host name or
   address may remain, and every `Authorization` header must be a placeholder.
6. Re-run the test. A re-recorded fixture that now fails may be listed in
   `expected_failures.txt`, with the line the test prints, in the same pull
   request; say why in its description.
