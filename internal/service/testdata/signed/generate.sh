#!/usr/bin/env bash
# Regenerates the signed commit fixtures with native git, gpg and ssh-keygen. CI needs none of the three: the output is
# committed. The keys are made here and used for nothing else.
set -euo pipefail

out=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export GNUPGHOME=$work/gnupg
mkdir -m 700 "$GNUPGHOME"

# Pins gpg's clock so a key can sign inside its lifetime long after it has expired.
cat >"$work/gpgfake" <<SH
#!/bin/sh
exec gpg --faked-system-time "\$GPG_FAKE_TIME" "\$@"
SH
chmod +x "$work/gpgfake"
export GPG_FAKE_TIME=20240115T120000

gpg_quiet() {
  gpg --batch --pinentry-mode loopback --passphrase '' --faked-system-time 20240101T000000 "$@" 2>/dev/null
}

primary_fpr() { gpg --list-keys --with-colons "$1" | awk -F: '/^fpr/ {print $10; exit}'; }

gpg_quiet --quick-generate-key "Rhea <rhea@example.com>" ed25519 sign never
rhea=$(primary_fpr rhea@example.com)
gpg_quiet --quick-add-key "$rhea" ed25519 sign never
rhea_sub=$(gpg --list-keys --with-colons "$rhea" | awk -F: '/^sub/ {f=1} f && /^fpr/ {print $10; exit}')
gpg_quiet --quick-generate-key "Sam <sam@example.com>" ed25519 sign 30d
sam=$(primary_fpr sam@example.com)
gpg --armor --export "$rhea" >"$out/rhea.pub.asc"
gpg --armor --export "$sam" >"$out/sam.pub.asc"

ssh-keygen -q -t ed25519 -N '' -C fixture -f "$work/ed" >/dev/null
cp "$work/ed.pub" "$out/ed25519.pub"

git init -q "$work/repo"
cd "$work/repo"
tree=$(git mktree </dev/null)

# kind (gpg|ssh|none), signer, committer email, date, name
commit() {
  local kind=$1 signer=$2 email=$3 date=$4 name=$5 cfg=(-c core.abbrev=7) sign=() sha
  case $kind in
    gpg) cfg=(-c "gpg.program=$work/gpgfake"); sign=(-S"$signer") ;;
    ssh) cfg=(-c gpg.format=ssh); sign=(-S"$signer") ;;
  esac
  sha=$(GIT_AUTHOR_NAME=Rhea GIT_AUTHOR_EMAIL=$email GIT_AUTHOR_DATE=$date \
    GIT_COMMITTER_NAME=Rhea GIT_COMMITTER_EMAIL=$email GIT_COMMITTER_DATE=$date \
    git "${cfg[@]}" commit-tree ${sign[@]+"${sign[@]}"} "$tree" -m "fixture $name")
  git cat-file commit "$sha" >"$out/$name.commit"
}

d=2024-01-15T12:00:00Z
commit none "" rhea@example.com $d unsigned
commit gpg "${rhea}!" rhea@example.com $d gpg-primary
commit gpg "${rhea_sub}!" rhea@example.com $d gpg-subkey
commit gpg "${rhea}!" rhea.work@example.com $d gpg-other-email
commit gpg "$sam" sam@example.com $d gpg-expiring-signed-inside
commit gpg "$sam" sam@example.com 2024-03-01T12:00:00Z gpg-expiring-dated-after
commit ssh "$work/ed" rhea@example.com $d ssh-ed25519
commit ssh "$work/ed" rhea@contractor.dev $d ssh-other-email
commit ssh "$work/ed" 7+rhea@users.noreply.localhost $d ssh-noreply
