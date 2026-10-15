#!/usr/bin/env python3
"""Synchronize docs/index.html, docs/openapi.json, and docs/swagger/ from canonical repo sources."""

from __future__ import annotations

import html
import json
import pathlib
import re
import shutil
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
VERSION_FILE = ROOT / "VERSION"
CHANGELOG_FILE = ROOT / "CHANGELOG.md"
ROADMAP_FILE = ROOT / "docs" / "specs" / "ROADMAP.md"
OPENAPI_SRC = ROOT / "server" / "internal" / "api" / "swagger" / "ui" / "openapi.json"
SWAGGER_UI_DIR = ROOT / "server" / "internal" / "api" / "swagger" / "ui"
DOCS_DIR = ROOT / "docs"
DOCS_INDEX = DOCS_DIR / "index.html"
DOCS_OPENAPI = DOCS_DIR / "openapi.json"
DOCS_SWAGGER_DIR = DOCS_DIR / "swagger"


def format_inline_md(text: str) -> str:
    """Convert basic inline Markdown (bold, code, links) into safe HTML."""
    escaped = html.escape(text, quote=False)
    code_tokens: list[str] = []

    def save_code(match: re.Match[str]) -> str:
        idx = len(code_tokens)
        code_tokens.append(f"<code>{match.group(1)}</code>")
        return f"\x00CODE{idx}\x00"

    # Inline code first so backticked '*' does not break **bold**
    escaped = re.sub(r"`([^`]+)`", save_code, escaped)

    # Links: [label](url)
    def repl_link(match: re.Match[str]) -> str:
        label, url = match.group(1), match.group(2)
        if not url.startswith(("http://", "https://", "#", "./", "../")):
            url = f"https://github.com/file4base/file4base-app/blob/main/{url.lstrip('/')}"
        return f'<a href="{url}" target="_blank" rel="noopener">{label}</a>'

    escaped = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", repl_link, escaped)
    # Bold: **text**
    escaped = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", escaped)
    # Italics: *text*
    escaped = re.sub(r"(?<!\*)\*([^*]+)\*(?!\*)", r"<em>\1</em>", escaped)

    for idx, token in enumerate(code_tokens):
        escaped = escaped.replace(f"\x00CODE{idx}\x00", token)
    return escaped


def build_openapi_block() -> tuple[str, int, int]:
    """Read openapi.json, copy to docs/openapi.json and docs/swagger/, and build summary HTML."""
    if not OPENAPI_SRC.is_file():
        return "", 0, 0

    spec = json.loads(OPENAPI_SRC.read_text(encoding="utf-8"))
    paths = spec.get("paths", {})
    total_paths = len(paths)
    http_methods = {"get", "post", "put", "patch", "delete"}
    total_ops = 0
    tag_counts: dict[str, int] = {}

    for _, path_item in paths.items():
        if not isinstance(path_item, dict):
            continue
        for method, op in path_item.items():
            if method.lower() in http_methods and isinstance(op, dict):
                total_ops += 1
                tags = op.get("tags") or ["General"]
                for tag in tags:
                    tag_counts[str(tag)] = tag_counts.get(str(tag), 0) + 1

    # Sync docs/openapi.json and docs/swagger/
    shutil.copyfile(OPENAPI_SRC, DOCS_OPENAPI)
    DOCS_SWAGGER_DIR.mkdir(parents=True, exist_ok=True)
    for asset in SWAGGER_UI_DIR.iterdir():
        if asset.is_file():
            target = DOCS_SWAGGER_DIR / asset.name
            if asset.name == "index.html":
                idx_html = asset.read_text(encoding="utf-8").replace(
                    'url: "/swagger/openapi.json"', 'url: "./openapi.json"'
                )
                target.write_text(idx_html, encoding="utf-8")
            else:
                shutil.copyfile(asset, target)

    tag_pills = " · ".join(
        f"<strong>{html.escape(tag)}</strong>: {count}"
        for tag, count in sorted(tag_counts.items(), key=lambda item: (-item[1], item[0]))
    )

    block = (
        "<!-- AUTO-OPENAPI:START -->\n"
        '        <div class="callout">\n'
        f"          <strong>Live OpenAPI 3.0.3 Metrics (Auto-Synced):</strong> "
        f"<code>{total_paths}</code> REST paths · <code>{total_ops}</code> HTTP operations.<br>\n"
        f"          <span style=\"font-size: 0.88rem;\">{tag_pills}</span><br>\n"
        '          👉 <a href="./swagger/" target="_blank" rel="noopener"><strong>Open Hosted Interactive Swagger UI</strong></a> · '
        '<a href="./openapi.json" target="_blank" rel="noopener">Download <code>openapi.json</code></a>\n'
        "        </div>\n"
        "        <!-- AUTO-OPENAPI:END -->"
    )
    return block, total_paths, total_ops


def build_roadmap_block() -> str:
    """Parse docs/specs/ROADMAP.md and return an HTML progress table."""
    if not ROADMAP_FILE.is_file():
        return ""

    phases: list[tuple[str, int, int]] = []
    current_phase = ""
    done_count = 0
    total_count = 0

    for raw_line in ROADMAP_FILE.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if line.startswith("## "):
            if current_phase and total_count > 0:
                phases.append((current_phase, done_count, total_count))
            current_phase = line[3:].strip()
            done_count = 0
            total_count = 0
        elif line.startswith("- [x]") or line.startswith("- [X]"):
            done_count += 1
            total_count += 1
        elif line.startswith("- [ ]"):
            total_count += 1

    if current_phase and total_count > 0:
        phases.append((current_phase, done_count, total_count))

    rows_html: list[str] = []
    overall_done = sum(d for _, d, _ in phases)
    overall_total = sum(t for _, _, t in phases)

    for title, done, total in phases:
        pct = int(round((done / total) * 100)) if total else 100
        status = "✅ Complete" if done == total else f"🔄 In Progress ({pct}%)"
        rows_html.append(
            "            <tr>\n"
            f"              <td><strong>{html.escape(title)}</strong></td>\n"
            f"              <td><code>{done} / {total}</code></td>\n"
            f"              <td>{status}</td>\n"
            "            </tr>"
        )

    overall_pct = int(round((overall_done / overall_total) * 100)) if overall_total else 100
    return (
        "<!-- AUTO-ROADMAP:START -->\n"
        f"        <p>Overall specification &amp; milestone progress synced from "
        f'<a href="https://github.com/file4base/file4base-app/blob/main/docs/specs/ROADMAP.md" target="_blank" rel="noopener"><code>docs/specs/ROADMAP.md</code></a>: '
        f"<strong>{overall_done} / {overall_total} milestones completed ({overall_pct}%)</strong>.</p>\n"
        '        <div class="table-wrap">\n'
        "          <table>\n"
        "            <thead>\n"
        "              <tr>\n"
        "                <th>Roadmap Phase / Domain</th>\n"
        "                <th>Milestones</th>\n"
        "                <th>Status</th>\n"
        "              </tr>\n"
        "            </thead>\n"
        "            <tbody>\n"
        + "\n".join(rows_html)
        + "\n            </tbody>\n"
        "          </table>\n"
        "        </div>\n"
        "        <!-- AUTO-ROADMAP:END -->"
    )


def build_changelog_block(max_releases: int = 5) -> str:
    """Parse the most recent releases from CHANGELOG.md and render HTML."""
    if not CHANGELOG_FILE.is_file():
        return ""

    releases: list[tuple[str, str, list[tuple[str, list[str]]]]] = []
    current_ver = ""
    current_date = ""
    current_cat = ""
    categories: list[tuple[str, list[str]]] = []
    current_items: list[str] = []

    for raw_line in CHANGELOG_FILE.read_text(encoding="utf-8").splitlines():
        ver_match = re.match(r"^## \[([^\]]+)\](?:\s*-\s*(\S+))?", raw_line)
        if ver_match:
            if current_ver and (categories or current_items):
                if current_cat and current_items:
                    categories.append((current_cat, current_items))
                if categories:
                    releases.append((current_ver, current_date, categories))
                if len(releases) >= max_releases:
                    break
            current_ver = ver_match.group(1).strip()
            current_date = (ver_match.group(2) or "").strip()
            current_cat = ""
            categories = []
            current_items = []
            continue

        if not current_ver or current_ver.lower() == "unreleased":
            continue

        cat_match = re.match(r"^###\s+(.+)$", raw_line)
        if cat_match:
            if current_cat and current_items:
                categories.append((current_cat, current_items))
            current_cat = cat_match.group(1).strip()
            current_items = []
            continue

        # Top-level bullet under ### Category
        bullet_match = re.match(r"^-\s+(.+)$", raw_line)
        if bullet_match and current_cat:
            item_text = bullet_match.group(1).strip().rstrip(":")
            current_items.append(item_text)

    if len(releases) < max_releases and current_ver and current_ver.lower() != "unreleased":
        if current_cat and current_items:
            categories.append((current_cat, current_items))
        if categories:
            releases.append((current_ver, current_date, categories))

    cards: list[str] = []
    for ver, date_str, cats in releases[:max_releases]:
        date_badge = f' <span style="color: var(--text-muted); font-weight: 400; font-size: 0.9rem;">({html.escape(date_str)})</span>' if date_str else ""
        cat_sections: list[str] = []
        for cat_name, items in cats:
            li_html = "\n".join(f"            <li>{format_inline_md(it)}</li>" for it in items[:8])
            cat_sections.append(
                f"          <p style=\"margin: 10px 0 4px; font-weight: 600; color: var(--text-main);\">{html.escape(cat_name)}</p>\n"
                f"          <ul>\n{li_html}\n          </ul>"
            )
        cards.append(
            '        <div style="border: 1px solid var(--border); border-radius: 10px; padding: 16px 20px; margin-bottom: 14px; background: var(--bg);">\n'
            f'          <h3 style="margin-top: 0;">Release <code>v{html.escape(ver)}</code>{date_badge}</h3>\n'
            + "\n".join(cat_sections)
            + "\n        </div>"
        )

    return (
        "<!-- AUTO-CHANGELOG:START -->\n"
        + "\n".join(cards)
        + '\n        <p>View the full history in <a href="https://github.com/file4base/file4base-app/blob/main/CHANGELOG.md" target="_blank" rel="noopener"><code>CHANGELOG.md</code></a>.</p>\n'
        + "        <!-- AUTO-CHANGELOG:END -->"
    )


def sync_docs() -> int:
    version = VERSION_FILE.read_text(encoding="utf-8").strip()
    content = DOCS_INDEX.read_text(encoding="utf-8")

    # 1. Sync version references
    content = re.sub(
        r'<span class="version-pill">v[^<]+</span>',
        f'<span class="version-pill">v{version}</span>',
        content,
    )
    content = re.sub(
        r"File4Base \(<strong>v[^<]+</strong>\)",
        f"File4Base (<strong>v{version}</strong>)",
        content,
    )
    content = re.sub(
        r"Multi-Factor Authentication \(v[0-9]+\.[0-9]+\.[0-9]+\)",
        f"Multi-Factor Authentication (v{version})",
        content,
    )
    content = re.sub(
        r"cloudresources/file4base-api:[0-9]+\.[0-9]+\.[0-9]+",
        f"cloudresources/file4base-api:{version}",
        content,
    )
    content = re.sub(
        r"cloudresources/file4base-web:[0-9]+\.[0-9]+\.[0-9]+",
        f"cloudresources/file4base-web:{version}",
        content,
    )
    content = re.sub(
        r"\(e\.g\. <code>v[0-9]+\.[0-9]+\.[0-9]+</code>\)",
        f"(e.g. <code>v{version}</code>)",
        content,
    )
    content = re.sub(
        r"Version v[0-9]+\.[0-9]+\.[0-9]+",
        f"Version v{version}",
        content,
    )

    # 2. Sync OpenAPI block + hosted Swagger UI
    openapi_block, total_paths, _ = build_openapi_block()
    if openapi_block and "<!-- AUTO-OPENAPI:START -->" in content:
        content = re.sub(
            r"<!-- AUTO-OPENAPI:START -->.*?<!-- AUTO-OPENAPI:END -->",
            lambda _: openapi_block,
            content,
            flags=re.DOTALL,
        )
    if total_paths > 0:
        content = re.sub(
            r"\(\d+ REST endpoints\)",
            f"({total_paths} REST endpoints)",
            content,
        )
        content = re.sub(
            r"REST API v1 Reference \(\d+ Endpoints\)",
            f"REST API v1 Reference ({total_paths} Endpoints)",
            content,
        )

    # 3. Sync Roadmap block
    roadmap_block = build_roadmap_block()
    if roadmap_block and "<!-- AUTO-ROADMAP:START -->" in content:
        content = re.sub(
            r"<!-- AUTO-ROADMAP:START -->.*?<!-- AUTO-ROADMAP:END -->",
            lambda _: roadmap_block,
            content,
            flags=re.DOTALL,
        )

    # 4. Sync Changelog block
    changelog_block = build_changelog_block()
    if changelog_block and "<!-- AUTO-CHANGELOG:START -->" in content:
        content = re.sub(
            r"<!-- AUTO-CHANGELOG:START -->.*?<!-- AUTO-CHANGELOG:END -->",
            lambda _: changelog_block,
            content,
            flags=re.DOTALL,
        )

    DOCS_INDEX.write_text(content, encoding="utf-8")
    print(f"Synced docs/index.html, docs/openapi.json, and docs/swagger/ for v{version}")
    return 0


if __name__ == "__main__":
    sys.exit(sync_docs())
