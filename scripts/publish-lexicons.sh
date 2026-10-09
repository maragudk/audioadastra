#!/usr/bin/env bash
# Publish new com.audioadastra.* lexicons to the atproto network, as audioadastra.com.
# The only sanctioned way to publish lexicons. Run it with `make publish-lexicons`.
#
# Publishing is permanent and network-visible, so this refuses unless every safeguard holds:
# up-to-date clean main, passing lexicon tests, DNS pointing at the account, explicit confirmation,
# and a fully green `goat lex status` afterwards. It only publishes new lexicons: it never passes
# `--update` or `--skip-dns-check` to goat.
#
# Written for the bash 3.2 that ships with macOS.

set -euo pipefail

readonly account="audioadastra.com"
# Always this directory: goat's default (lexicons/) includes the record fixtures in lexicons/testdata.
readonly lexicons_dir="lexicons/com"
readonly test_tags="sqlite_fts5,sqlite_math_functions"

# goat loads .env from the working directory without overriding variables that are already set,
# so pin the PLC directory here to stop a local-network setting from leaking in.
export ATP_PLC_HOST="https://plc.directory"
unset GOAT_USERNAME ATP_USERNAME ATP_AUTH_USERNAME GOAT_PASSWORD ATP_PASSWORD ATP_AUTH_PASSWORD

cd "$(dirname "$0")/.."

fail() {
	echo "publish-lexicons: $*" >&2
	exit 1
}

step() {
	echo
	echo "==> $*"
}

# check_git_state <commit>
# With an empty commit, fetches origin and requires HEAD to equal origin/main. Otherwise requires
# HEAD to still be that commit, so nothing changed while the checks and prompts ran.
check_git_state() {
	local expected="$1" branch origin_url flagged
	branch="$(git symbolic-ref --quiet --short HEAD || true)"
	[ "$branch" = "main" ] || fail "must be on branch main, not '${branch:-detached HEAD}'"
	origin_url="$(git remote get-url origin || true)"
	case "$origin_url" in
	git@github.com:maragudk/audioadastra.git | ssh://git@github.com/maragudk/audioadastra.git | \
		https://github.com/maragudk/audioadastra.git | https://github.com/maragudk/audioadastra) ;;
	*) fail "origin must be github.com/maragudk/audioadastra, not '$origin_url'" ;;
	esac
	[ -z "$(git status --porcelain --untracked-files=all)" ] || {
		git status --short --untracked-files=all >&2
		fail "working tree is not clean"
	}
	[ -z "$(git ls-files --others --ignored --exclude-standard -- lexicons)" ] || {
		git ls-files --others --ignored --exclude-standard -- lexicons >&2
		fail "ignored files under lexicons/ would be published"
	}
	# git status does not show local edits to files marked skip-worktree (S) or assume-unchanged
	# (lowercase), so refuse those too.
	flagged="$(git ls-files -v -- lexicons | grep -v '^H ' || true)"
	[ -z "$flagged" ] || {
		echo "$flagged" >&2
		fail "files under lexicons/ are marked skip-worktree or assume-unchanged"
	}
	if [ -z "$expected" ]; then
		git fetch --quiet origin main || fail "could not fetch origin/main"
		[ "$(git rev-parse HEAD)" = "$(git rev-parse refs/remotes/origin/main)" ] ||
			fail "local main ($(git rev-parse --short HEAD)) is not origin/main ($(git rev-parse --short refs/remotes/origin/main))"
	else
		[ "$(git rev-parse HEAD)" = "$expected" ] || fail "HEAD moved from $expected while publishing was prepared"
	fi
}

# check_status_output <output> <allowed symbols> <reason>
# goat lex status always exits 0, so its output is parsed instead. From goat's lex_status.go:
#   🟢 local and published schemas are identical
#   🟠 local schema is not published yet
#   🟣 local schema differs from the published one
#   ⭕ published schema has no local file
# Every line must start with one of the allowed symbols; anything else, including an unknown line
# format, fails.
check_status_output() {
	local output="$1" allowed="$2" reason="$3" line symbol ok
	if [ -z "$output" ]; then
		fail "goat lex status printed nothing"
	fi
	while IFS= read -r line; do
		ok=""
		for symbol in $allowed; do
			case "$line" in
			" $symbol "*) ok=1 ;;
			esac
		done
		if [ -z "$ok" ]; then
			fail "$reason: '$line' (allowed: $allowed)"
		fi
	done <<EOF
$output
EOF
}

command -v goat >/dev/null || fail "goat is not installed"
[ -t 0 ] || fail "must be run interactively, from a terminal"

step "Checking git state"
check_git_state ""
commit="$(git rev-parse HEAD)"
echo "On main at $(git rev-parse --short HEAD), clean and equal to origin/main."

step "Running lexicon tests"
go test -tags "$test_tags" -count 1 ./lexicons/... || fail "lexicon tests failed"

step "Checking DNS for $lexicons_dir"
# goat lex check-dns exits 0 even when NSIDs do not resolve, so check its output instead.
dns_output="$(goat lex check-dns "$lexicons_dir")" || fail "goat lex check-dns failed"
echo "$dns_output"
[ "$dns_output" = "all lexicon schema NSIDs resolved successfully" ] || fail "not all lexicon NSIDs resolve via DNS"

step "Status before publishing"
status_output="$(goat lex status "$lexicons_dir")" || fail "goat lex status failed"
echo "$status_output"
# Changed (🟣) or remote-only (⭕) schemas would need --update or a deletion, which this never does.
check_status_output "$status_output" "🟢 🟠" "changed or remote-only lexicons need an update, which this never does"
case "$status_output" in
*" 🟠 "*) ;;
*)
	echo "Nothing to publish: every lexicon is already published and in sync."
	exit 0
	;;
esac

step "Checking that every lexicon's DNS points at $account"
# check-dns only checks that NSIDs resolve to some DID. goat lex publish skips schemas whose DNS
# points at another account (⭕) but still publishes the rest, so check before anything is sent.
account_did="$(goat resolve --did "$account" </dev/null)" || fail "could not resolve $account"
while read -r _ nsid; do
	nsid_did="$(goat lex resolve --did "$nsid" </dev/null)" || fail "could not resolve $nsid via DNS"
	[ "$nsid_did" = "$account_did" ] || fail "$nsid resolves to $nsid_did, not to $account ($account_did)"
	echo "$nsid -> $nsid_did"
done <<EOF
$status_output
EOF

echo
read -r -p "Publish? [y/N] " answer || answer=""
case "$answer" in
y | Y) ;;
*) fail "aborted, nothing published" ;;
esac

# goat falls back to its saved session (possibly another account) when the password is empty,
# so an empty password must never reach it.
# Echo is turned off before the prompt is printed: `read -s` only turns it off after printing,
# so input typed right as the prompt appears would show.
trap 'stty echo' EXIT
stty -echo
printf "App password for %s: " "$account" >&2
IFS= read -r password || password=""
stty echo
trap - EXIT
echo >&2
[ -n "$password" ] || fail "no app password given, nothing published"

step "Publishing $lexicons_dir as $account"
check_git_state "$commit"
# The password goes through the environment of this single goat invocation, not argv, so it
# does not show in ps. With both username and password set, goat logs in with an ephemeral
# session and neither reads nor writes its saved session.
publish_output="$(GOAT_PASSWORD="$password" goat lex publish --username "$account" "$lexicons_dir")" || {
	unset password
	# Anything goat printed before failing (🟢 lines) is already published.
	echo "$publish_output"
	fail "goat lex publish failed, check the lines above for anything already published"
}
unset password
echo "$publish_output"
# From goat's lex_publish.go: ⭕ means the NSID group's DNS points at another account, so the
# schema was skipped. goat still exits 0.
case "$publish_output" in
*"⭕"*) fail "some lexicons were skipped because their DNS does not point at $account" ;;
esac

step "Status after publishing"
status_output="$(goat lex status "$lexicons_dir")" || fail "goat lex status failed"
echo "$status_output"
check_status_output "$status_output" "🟢" "not every lexicon is in sync after publishing"

echo
echo "Published. Every lexicon in $lexicons_dir is in sync with the network."
