#!/usr/bin/env bash
set -Eeuo pipefail
export HOME=/tmp
# Public demonstration subscriber from the pinned lab values. Preserve existing data.
if mongosh "${DB_URI:?DB_URI is required}" --quiet --eval 'if (db.subscribers.findOne({imsi: "999700000000001"})) { quit(0); } quit(42);'; then
  echo 'Demo subscriber already exists; keeping it.'
else
  result=$?
  if [[ "$result" != 42 ]]; then exit "$result"; fi
  open5gs-dbctl add_ue_with_apn 999700000000001 465B5CE8B199B49FAA5F0A2EE238A6BC E8ED289DEBA952E4283B54E88E6183CA internet
fi
