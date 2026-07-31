#!/usr/bin/env bash

set -euo pipefail

RELEASE_BRANCH="main"
TAG_PATTERN='^v([0-9]+)\.([0-9]+)\.([0-9]+)$'

usage() {
    cat <<'EOF'
Usage:
  release.sh release [--dry-run] [--major|--minor|--patch]

Tag origin/main with the next CLI release version.

  --dry-run                    Print the proposed release and exit.
  --major|--minor|--patch      Version segment to increase (default: minor).

The first release defaults to v0.1.0.
EOF
}

get_last_tag() {
    local tag
    while IFS= read -r tag; do
        if [[ "$tag" =~ $TAG_PATTERN ]]; then
            printf '%s\n' "$tag"
            return
        fi
    done < <(git tag --sort=-v:refname)
}

next_tag() {
    local last_tag="$1"
    local bump="$2"

    if [[ -z "$last_tag" ]]; then
        case "$bump" in
            major) printf '%s\n' 'v1.0.0' ;;
            minor) printf '%s\n' 'v0.1.0' ;;
            patch) printf '%s\n' 'v0.0.1' ;;
        esac
        return
    fi

    if [[ ! "$last_tag" =~ $TAG_PATTERN ]]; then
        echo "Error: last tag '$last_tag' is not a semantic version tag." >&2
        exit 1
    fi

    local major="${BASH_REMATCH[1]}"
    local minor="${BASH_REMATCH[2]}"
    local patch="${BASH_REMATCH[3]}"

    case "$bump" in
        major) major=$((major + 1)); minor=0; patch=0 ;;
        minor) minor=$((minor + 1)); patch=0 ;;
        patch) patch=$((patch + 1)) ;;
    esac

    printf 'v%d.%d.%d\n' "$major" "$minor" "$patch"
}

assert_tag_unused() {
    local tag="$1"

    if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
        echo "Error: tag ${tag} already exists locally." >&2
        exit 1
    fi

    if git ls-remote --exit-code --tags origin "refs/tags/${tag}" >/dev/null 2>&1; then
        echo "Error: tag ${tag} already exists on origin." >&2
        exit 1
    fi
}

confirm() {
    local answer
    read -r -p "Proceed? [y/N] " answer
    if [[ ! "$answer" =~ ^[Yy]$ ]]; then
        echo "Aborted. Nothing was changed."
        exit 0
    fi
}

tag_release() {
    local dry_run="$1"
    local bump="$2"

    git fetch origin "$RELEASE_BRANCH" --tags --quiet

    local last_tag new_tag
    last_tag=$(get_last_tag)
    new_tag=$(next_tag "$last_tag" "$bump")

    assert_tag_unused "$new_tag"

    echo
    echo "Release target: origin/${RELEASE_BRANCH}"
    echo "Last tag:       ${last_tag:-none (first release)}"
    echo "New tag:        ${new_tag} (${bump})"
    echo "Actions:        tag origin/${RELEASE_BRANCH} -> push tag"
    echo

    if [[ "$dry_run" == true ]]; then
        echo "DRY RUN - nothing was changed."
        return
    fi

    confirm
    git tag "$new_tag" "origin/${RELEASE_BRANCH}"
    git push origin "$new_tag"
    echo "✓ Tag ${new_tag} created and pushed."
}

command="${1:-}"
shift || true

if [[ "$command" != "release" ]]; then
    usage
    exit 1
fi

dry_run=false
bump="minor"
bump_explicit=false
for argument in "$@"; do
    case "$argument" in
        --dry-run) dry_run=true ;;
        --major|--minor|--patch)
            if [[ "$bump_explicit" == true ]]; then
                echo "Error: --major, --minor, and --patch are mutually exclusive." >&2
                exit 1
            fi
            bump="${argument#--}"
            bump_explicit=true
            ;;
        --help|-h) usage; exit 0 ;;
        *)
            echo "Error: unknown argument '$argument'." >&2
            usage >&2
            exit 1
            ;;
    esac
done

tag_release "$dry_run" "$bump"
