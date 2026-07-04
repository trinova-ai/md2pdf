#!/bin/sh
# release-picoloom.sh — cut a tagged release of the picoloom fork.
#
# Usage: scripts/release-picoloom.sh <tag> [fork-dir]
#   <tag>       e.g. v2.1.2-trinova.2 — NEVER reuse a published tag: the Go
#               checksum database records it permanently.
#   [fork-dir]  fork checkout, default ./picoloom
#
# The fork's `trinova` branch stays pure (upstream + patches, PR-able). This
# script stamps a *generated* module-rename commit on a detached head above
# it, tags that commit, pushes the tag, and leaves the checkout resting on
# the regenerated `dev` branch (what the go.work dev loop builds against).
# Finally it bumps md2pdf's go.mod to the new tag — review and commit that.
set -eu

TAG=${1:?usage: release-picoloom.sh <tag> [fork-dir]}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
FORK=${2:-$ROOT/picoloom}
OLD=github.com/alnah/picoloom/v2
NEW=github.com/trinova-ai/picoloom/v2

cd "$FORK"
[ -z "$(git status --porcelain)" ] || { echo "release-picoloom: fork tree not clean" >&2; exit 1; }
if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
	echo "release-picoloom: tag $TAG already exists — published tags are immutable, mint the next one" >&2
	exit 1
fi

git checkout -q --detach trinova
go mod edit -module "$NEW"
find . -name '*.go' -not -path './.git/*' -exec sed -i '' "s|$OLD|$NEW|g" {} +
GOWORK=off go build ./...
GOWORK=off go test ./...
git commit -aqm "chore(release): module path $NEW for $TAG"
git tag "$TAG"
git push -q origin "$TAG"
git branch -f dev HEAD
git checkout -q dev

cd "$ROOT"
GOWORK=off go get "$NEW@$TAG"
GOWORK=off go mod tidy
echo "release-picoloom: $TAG published; go.mod requires $NEW@$TAG — review and commit"
