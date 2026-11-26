# Contributing to File4Base

Read [AGENTS.md](AGENTS.md) for the repository's English-language, changelog,
versioning and documentation rules. Submit changes through a pull request with
the problem, resulting behavior and relevant validation.

## Contribution rights

Submit original contributions under the project's **GPL-3.0-only** terms, unless
an existing file or separately identified third-party component has other
applicable terms. Keep those terms and required notices intact. Flag any
proposed license change explicitly for maintainer review.

Confirm that you have the right to contribute the code, documentation, artwork
and example data. Where an employer, client or another author holds rights,
obtain the necessary authorization. This contribution policy does not assign
copyright to the project or replace third-party permissions.

## References and AI-assisted contributions

Follow [the reference and asset provenance practice](docs/LEGAL.md#reference-and-asset-provenance).
Identify external source titles, versions and URLs accurately, explain what
they informed, and distinguish functional requirements from imported expression.
For AI-assisted work, record relevant tools and reference inputs where known
and review the output for rights, correctness and dependency notices. AI
generation alone does not establish authorship or permission.

Develop File4Base's own interface composition, explanations, artwork and sample
scenarios. Preserve useful common commands and justified compatibility. Do not
copy proprietary code, documentation, screenshots, icons or sample files unless
the applicable permission or legal basis has been reviewed and documented.

Keep confidential reference material out of public issues and PRs. Describe
unresolved provenance honestly and ask a maintainer to arrange private review
when needed. Preserve truthful history rather than deleting references to hide
how a contribution was developed.

## Development environment

### Database access from the host

The compose stacks never publish the database: only the API reaches it, inside
`file4base-net`. To run a local `go run ./cmd/server` against the compose
database, publish it on the loopback interface with a
`docker-compose.override.yml` next to `docker-compose.yml`. Docker Compose
reads that file automatically, and `.gitignore` keeps it out of commits:

```yaml
# docker-compose.override.yml (development only, never on a server)
services:
  postgres:
    ports:
      - "127.0.0.1:5432:5432"
```

The MariaDB stack does not read the override automatically: name both files
(`docker compose -f docker-compose.mariadb.yml -f mariadb.override.yml up -d`),
with `mariadb:` and `127.0.0.1:3306:3306` in the override.

The server has no built-in `DATABASE_URL`: set it for `go run`. The Go tests
use their own DSNs (`F4B_TEST_DEV_DSN`, `F4B_TEST_ADMIN_DSN`, `dev_password`
on `localhost:5432` by default), so a throwaway test database is the simplest
setup for them:

```bash
docker run -d --rm --name f4b-test-pg -e POSTGRES_USER=file4base \
  -e POSTGRES_PASSWORD=dev_password -e POSTGRES_DB=file4base_dev \
  -p 127.0.0.1:5432:5432 postgres:16-alpine
cd server && go test -p 1 ./...
```

### Web Client development

The Web Client calls the API on the origin it is served from. When it runs
from `flutter run -d chrome` instead of the `file4base-web` image, set the
server address in the connection dialog and allow the development origin on
the API, e.g. `CORS_ALLOWED_ORIGINS=http://localhost:5000` with
`flutter run -d chrome --web-port 5000`.

`scripts/tests/compose_defaults_test.sh` checks that both compose stacks keep
their secure defaults: no default password, no published database port, no
CORS and no public database creation unless `.env` sets them.

## Pull request review

- Explain what changed and validate the relevant behavior or documentation.
- Identify new/changed external material and its rights/notice requirements.
- For UI or tutorial changes, explain the independent design choices and any
  necessary compatibility constraints.
- For packaging/dependency changes, inspect the final distributed legal bundle,
  including native dependencies downloaded during the build.
- Link unresolved rights questions rather than claiming they are cleared.

The [legal practices](docs/LEGAL.md) explain the legal basis for these checks.
They are proportionate project review requirements, not a claim that a CLA,
rights assignment or clean-room development process is universally mandatory.
