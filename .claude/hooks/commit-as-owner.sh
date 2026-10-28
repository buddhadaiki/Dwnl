#!/usr/bin/env bash
# Claude Code cloud sessions commit as "Claude <noreply@anthropic.com>", which
# lists the claude account among this repository's contributors. When that is
# the identity git would use, commit as the repository owner instead; any other
# identity (a local session, another person) is left untouched.
name="Gabriel Souza"
email="167637305+buddhadaiki@users.noreply.github.com"

git var GIT_AUTHOR_IDENT 2>/dev/null | grep -q "noreply@anthropic.com" || exit 0

git config user.name "$name"
git config user.email "$email"
# Variables beat git config, so also export them to the session's commands.
if [ -n "$CLAUDE_ENV_FILE" ]; then
  printf 'export GIT_AUTHOR_NAME=%q GIT_AUTHOR_EMAIL=%q GIT_COMMITTER_NAME=%q GIT_COMMITTER_EMAIL=%q\n' \
    "$name" "$email" "$name" "$email" >> "$CLAUDE_ENV_FILE"
fi
