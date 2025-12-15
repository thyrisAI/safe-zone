#!/usr/bin/env bash
set -euo pipefail

BUMP="patch"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bump)
      BUMP="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ "$BUMP" != "patch" && "$BUMP" != "minor" && "$BUMP" != "major" ]]; then
  echo "Invalid bump type: $BUMP (expected patch|minor|major)" >&2
  exit 1
fi

echo "[tszclient-py] Release bump type: $BUMP"

# Find last tszclient-py tag
LAST_TAG=$(git tag --list 'tszclient-py-v*' | sort -V | tail -n 1 || true)

if [[ -z "$LAST_TAG" ]]; then
  BASE_VERSION="0.0.0"
  echo "[tszclient-py] No previous tag found, starting from $BASE_VERSION"
else
  BASE_VERSION=${LAST_TAG#tszclient-py-v}
  echo "[tszclient-py] Last tag: $LAST_TAG (base version: $BASE_VERSION)"
fi

# Check for changes in Python client
if [[ -n "$LAST_TAG" ]]; then
  if git diff --quiet "$LAST_TAG"..HEAD -- pkg/tszclient_py pyproject.toml; then
    echo "[tszclient-py] No changes since $LAST_TAG in pkg/tszclient_py or pyproject.toml, skipping release."
    exit 0
  fi
else
  echo "[tszclient-py] No previous tag, will release initial version."
fi

# Read current version from pyproject.toml
CURRENT_PY_VERSION=$(python - << 'PY'
import tomllib
from pathlib import Path

pyproject = tomllib.loads(Path('pyproject.toml').read_text('utf-8'))
print(pyproject['project']['version'])
PY
)

echo "[tszclient-py] Current pyproject version: $CURRENT_PY_VERSION"

IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_PY_VERSION"

case "$BUMP" in
  major)
    MAJOR=$((MAJOR + 1))
    MINOR=0
    PATCH=0
    ;;
  minor)
    MINOR=$((MINOR + 1))
    PATCH=0
    ;;
  patch)
    PATCH=$((PATCH + 1))
    ;;
 esac

NEW_VERSION="$MAJOR.$MINOR.$PATCH"
NEW_TAG="tszclient-py-v$NEW_VERSION"

echo "[tszclient-py] New version: $NEW_VERSION (tag: $NEW_TAG)"

# Update version in pyproject.toml
python - << PY
from pathlib import Path
import re

path = Path('pyproject.toml')
text = path.read_text('utf-8')

text_new = re.sub(r'^(version\s*=\s*\")[0-9]+\.[0-9]+\.[0-9]+(\")', r"\\g<1>" + "${NEW_VERSION}" + r"\\g<2>", text, flags=re.MULTILINE)

path.write_text(text_new, 'utf-8')
PY

# Run tests (if any)
echo "[tszclient-py] Running tests (if configured)..."
# Add your test command here if you have one, for example: pytest

# Ensure build/twine are available
# 1) First, check if build and twine modules are importable
# 2) If missing, try to install them via pip; if pip is not available, exit with a clear message

echo "[tszclient-py] Checking for 'build' and 'twine' modules..."
if ! python - << 'PY'
import importlib.util, sys
missing = []
for name in ("build", "twine"):
    if importlib.util.find_spec(name) is None:
        missing.append(name)
if missing:
    sys.exit(1)
PY
then
  echo "[tszclient-py] Python modules 'build' and/or 'twine' are missing. Trying to install via pip..."
  if python -m pip --version >/dev/null 2>&1; then
    python -m pip install --upgrade pip
    python -m pip install build twine
  else
    echo "[tszclient-py] 'pip' is not available for this Python interpreter."
    echo "[tszclient-py] Please install 'build' and 'twine' manually or enable pip (e.g. 'py -m ensurepip --upgrade') before running this script."
    exit 1
  fi
fi

echo "[tszclient-py] Building package..."
python -m build

echo "[tszclient-py] Uploading to PyPI..."
python -m twine upload dist/* -u __token__ -p "$PYPI_TOKEN"

# Create and push tag
echo "[tszclient-py] Creating git tag $NEW_TAG"
git tag "$NEW_TAG"

echo "[tszclient-py] Pushing tag to origin"
git push origin "$NEW_TAG"

echo "[tszclient-py] Release completed."
