#!/usr/bin/env bash
set -euo pipefail

BUMP="patch"
FORCE="false"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bump)
      BUMP="$2"
      shift 2
      ;;
    --force)
      FORCE="true"
      shift 1
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

echo "[tsz-cli] Release bump type: $BUMP (force=$FORCE)"

# Find last tsz-cli tag
LAST_TAG=$(git tag --list 'tsz-cli-v*' | sort -V | tail -n 1 || true)

if [[ -z "$LAST_TAG" ]]; then
  BASE_VERSION="0.0.0"
  echo "[tsz-cli] No previous tag found, starting from $BASE_VERSION"
else
  BASE_VERSION=${LAST_TAG#tsz-cli-v}
  echo "[tsz-cli] Last tag: $LAST_TAG (base version: $BASE_VERSION)"
fi

# Check for changes in CLI code, unless forced
if [[ "$FORCE" != "true" && -n "$LAST_TAG" ]]; then
  if git diff --quiet "$LAST_TAG"..HEAD -- pkg/tsz-cli; then
    echo "[tsz-cli] No changes since $LAST_TAG in CLI, skipping release. (use --force to override)"
    exit 0
  fi
elif [[ "$FORCE" != "true" && -z "$LAST_TAG" ]]; then
  echo "[tsz-cli] No previous tag, will release initial version."
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
NEW_TAG="tsz-cli-v$NEW_VERSION"

echo "[tsz-cli] New version: $NEW_VERSION (tag: $NEW_TAG)"

# Create and push tag
echo "[tsz-cli] Creating git tag $NEW_TAG"
git tag "$NEW_TAG"

echo "[tsz-cli] Pushing tag to origin"
git push origin "$NEW_TAG"

echo "[tsz-cli] Release completed."
