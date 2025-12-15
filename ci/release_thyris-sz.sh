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

echo "[thyris-sz] Release bump type: $BUMP"

# Find last thyris-sz tag
LAST_TAG=$(git tag --list 'thyris-sz-v*' | sort -V | tail -n 1 || true)

if [[ -z "$LAST_TAG" ]]; then
  BASE_VERSION="0.0.0"
  echo "[thyris-sz] No previous tag found, starting from $BASE_VERSION"
else
  BASE_VERSION=${LAST_TAG#thyris-sz-v}
  echo "[thyris-sz] Last tag: $LAST_TAG (base version: $BASE_VERSION)"
fi

# Check for changes relevant to thyris-sz
# Mantık: thyris-sz = client klasörleri dışındaki her şey.
# Yani sadece client'lar (pkg/tszclient-go, pkg/tszclient_py) değiştiyse thyris-sz release'i tetiklenmesin.
if [[ -n "$LAST_TAG" ]]; then
  if git diff --quiet "$LAST_TAG"..HEAD -- . ':(exclude)pkg/tszclient-go' ':(exclude)pkg/tszclient_py'; then
    echo "[thyris-sz] No changes since $LAST_TAG outside client directories, skipping release."
    exit 0
  fi
else
  echo "[thyris-sz] No previous tag, will release initial version."
fi

IFS='.' read -r MAJOR MINOR PATCH <<< "$BASE_VERSION"

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
NEW_TAG="thyris-sz-v$NEW_VERSION"

echo "[thyris-sz] New version: $NEW_VERSION (tag: $NEW_TAG)"

# Run tests
echo "[thyris-sz] Running tests..."
go test ./...

# Build binary (customize as needed)
echo "[thyris-sz] Building binary..."
go build -o thyris-sz ./...

# Create and push tag
echo "[thyris-sz] Creating git tag $NEW_TAG"
git tag "$NEW_TAG"

echo "[thyris-sz] Pushing tag to origin"
git push origin "$NEW_TAG"

echo "[thyris-sz] Release completed."
