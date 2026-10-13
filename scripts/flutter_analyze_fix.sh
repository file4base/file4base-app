#!/usr/bin/env bash
# Runs very_good_analysis static analysis and optional automated code fixes on the Flutter client.
# Usage:
#   ./scripts/flutter_analyze_fix.sh            # Dry-run fixes + flutter analyze
#   ./scripts/flutter_analyze_fix.sh --apply    # Apply automated fixes, format, and analyze

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLIENT_DIR="${ROOT_DIR}/client"

APPLY_FIXES=false
if [[ "${1:-}" == "--apply" ]]; then
  APPLY_FIXES=true
fi

cd "${CLIENT_DIR}"

if [[ "${APPLY_FIXES}" == "true" ]]; then
  echo "==> Applying automated Dart/Flutter fixes (very_good_analysis)..."
  dart fix --apply
  echo "==> Formatting Dart code..."
  dart format .
else
  echo "==> Previewing automated Dart/Flutter fixes (dry-run)..."
  dart fix --dry-run
fi

echo "==> Running Flutter static analysis (very_good_analysis)..."
flutter analyze --no-fatal-infos
