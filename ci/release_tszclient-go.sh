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

echo "[tszclient-go] Release bump type: $BUMP"

# Find last tszclient-go tag
LAST_TAG=$(git tag --list 'tszclient-go-v*' | sort -V | tail -n 1 || true)

if [[ -z "$LAST_TAG" ]]; then
  BASE_VERSION="0.0.0"
  echo "[tszclient-go] No previous tag found, starting from $BASE_VERSION"
else
  BASE_VERSION=${LAST_TAG#tszclient-go-v}
  echo "[tszclient-go] Last tag: $LAST_TAG (base version: $BASE_VERSION)"
fi

# Check for changes in Go client code or related tests
if [[ -n "$LAST_TAG" ]]; then
  if git diff --quiet "$LAST_TAG"..HEAD -- pkg/tszclient-go tests/unit/tszclient_go_chat_test.go; then
    echo "[tszclient-go] No changes since $LAST_TAG in Go client or its tests, skipping release."
    exit 0
  fi
else
  echo "[tszclient-go] No previous tag, will release initial version."
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
NEW_TAG="tszclient-go-v$NEW_VERSION"

echo "[tszclient-go] New version: $NEW_VERSION (tag: $NEW_TAG)"

# Run tests for the whole module (pure Go, no Python involved)
echo "[tszclient-go] Running tests..."
go test ./...

# Create and push tag
echo "[tszclient-go] Creating git tag $NEW_TAG"
git tag "$NEW_TAG"

echo "[tszclient-go] Pushing tag to origin"
git push origin "$NEW_TAG"

echo "[tszclient-go] Release completed."
