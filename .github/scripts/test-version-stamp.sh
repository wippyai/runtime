#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0

set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp_dir="$repo_root/.version-stamp-test.$$"
target_file="$tmp_dir/print-version.mk"
make_bin="$(command -v make)"
go_bin="$(command -v go)"
failures=0

mkdir -p "$tmp_dir"
trap 'rm -rf "$tmp_dir"' EXIT

cat > "$target_file" <<'EOF'
.PHONY: print-wippy-version
print-wippy-version:
	@printf '%s\n' '$(WIPPY_VERSION)'
EOF

new_fixture() {
	local dir="$1"
	mkdir -p "$dir"
	git -C "$dir" init -q
	git -C "$dir" config user.name "Wippy Version Test"
	git -C "$dir" config user.email "version-test@example.invalid"
	printf 'base\n' > "$dir/source.txt"
	git -C "$dir" add source.txt
	git -C "$dir" commit -q -m base
}

resolve_default() {
	local dir="$1"
	shift
	env -u WIPPY_VERSION "$@" "$make_bin" -s -C "$dir" -f "$repo_root/Makefile" -f "$target_file" print-wippy-version
}

expect_version() {
	local name="$1"
	local expected="$2"
	local actual
	shift 2
	if actual=$("$@"); then
		if [[ "$actual" == "$expected" ]]; then
			printf 'PASS %s: %s\n' "$name" "$actual"
		else
			printf 'FAIL %s: expected <%s>, got <%s>\n' "$name" "$expected" "$actual"
			failures=$((failures + 1))
		fi
	else
		printf 'FAIL %s: Makefile evaluation failed\n' "$name"
		failures=$((failures + 1))
	fi
}

tagged="$tmp_dir/tagged"
new_fixture "$tagged"
git -C "$tagged" tag v0.3.43a
expect_version clean-tag dev-v0.3.43a resolve_default "$tagged"

printf 'after tag\n' >> "$tagged/source.txt"
git -C "$tagged" commit -q -am after-tag
after_tag="$(git -C "$tagged" rev-parse --short=7 HEAD)"
expect_version after-tag "dev-v0.3.43a-1-g${after_tag}" resolve_default "$tagged"

printf 'dirty\n' >> "$tagged/source.txt"
dirty_description="$(git -C "$tagged" describe --tags --always --dirty)"
expect_version tracked-dirty "dev-${dirty_description}" resolve_default "$tagged"

no_tags="$tmp_dir/no-tags"
new_fixture "$no_tags"
no_tags_commit="$(git -C "$no_tags" rev-parse --short=7 HEAD)"
expect_version no-tags "dev-${no_tags_commit}" resolve_default "$no_tags"

no_repo="$tmp_dir/no-repo"
mkdir -p "$no_repo"
expect_version no-repository dev resolve_default "$no_repo" env "GIT_CEILING_DIRECTORIES=$tmp_dir"

no_git_bin="$tmp_dir/no-git-bin"
mkdir -p "$no_git_bin"
cat > "$no_git_bin/git" <<'EOF'
#!/bin/sh
exit 127
EOF
chmod +x "$no_git_bin/git"
expect_version git-unavailable dev resolve_default "$tagged" env "PATH=$no_git_bin:$PATH"
expect_version explicit-override release-identity env WIPPY_VERSION=release-identity "$make_bin" -s -C "$tagged" -f "$repo_root/Makefile" -f "$target_file" print-wippy-version

run_linked_test() {
	local stamp="$1"
	if WIPPY_TEST_EXPECT_VERSION="$stamp" "$go_bin" test -count=1 \
		-ldflags="-X github.com/wippyai/runtime/api/version.Version=$stamp" \
		./runtime/lua/modules/system -run '^TestVersionLinkedStamp$'; then
		printf 'PASS linked Lua stamp: %s\n' "$stamp"
	else
		printf 'FAIL linked Lua stamp: %s\n' "$stamp"
		failures=$((failures + 1))
	fi
}

if env -u WIPPY_TEST_EXPECT_VERSION "$go_bin" test -count=1 ./runtime/lua/modules/system -run '^TestVersionLinkedStamp$'; then
	printf 'PASS unstamped Lua stamp: dev\n'
else
	printf 'FAIL unstamped Lua stamp: dev\n'
	failures=$((failures + 1))
fi

run_linked_test dev
run_linked_test v0.3.43a
run_linked_test dev-v0.3.43a-5-gabcdef0-dirty
run_linked_test nightly-20260925-3f84d88

cli_stamp=v0.3.43a
cli_bin="$tmp_dir/wippy"
if "$go_bin" build -tags "fts5 sqlite_vec treesitter sqlite_preupdate_hook" \
	-ldflags="-X github.com/wippyai/runtime/api/version.Version=$cli_stamp" \
	-o "$cli_bin" ./cmd/wippy/; then
	if cli_output=$("$cli_bin" version --short) && [[ "$cli_output" == "$cli_stamp" ]]; then
		printf 'PASS CLI short version: %s\n' "$cli_output"
	else
		printf 'FAIL CLI short version: expected <%s>, got <%s>\n' "$cli_stamp" "$cli_output"
		failures=$((failures + 1))
	fi
else
	printf 'FAIL CLI build for version stamp smoke check\n'
	failures=$((failures + 1))
fi

if (( failures != 0 )); then
	printf '%d version stamp checks failed\n' "$failures"
	exit 1
fi

printf 'All version stamp checks passed\n'
