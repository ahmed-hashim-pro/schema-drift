#!/usr/bin/env bash
# A pull request against a contracts repository, as CI would see it.
#
# main holds orders v1-v2 and payments v1. The pull request edits orders v1
# in place (adds GBP), adds orders v3 (amount becomes a decimal string) and
# payments v2 (a new status). Each is a classic way to break a consumer.
#
#   examples/gate-demo.sh            (schema-drift on PATH, or SCHEMA_DRIFT=path)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
bin=${SCHEMA_DRIFT:-schema-drift}
command -v "$bin" > /dev/null || bin=$(cd "$here/.." && pwd)/schema-drift

repo=$(mktemp -d "${TMPDIR:-/tmp}/schema-drift-demo.XXXXXX")
trap 'rm -rf -- "$repo"' EXIT
cd "$repo"
git init -q -b main
mkdir -p contracts/orders contracts/payments
cp "$here"/contracts/orders/{mode,v1.json,v2.json} contracts/orders/
cp "$here"/contracts/payments/{mode,v1.json} contracts/payments/
git add -A
git -c user.name=demo -c user.email=demo@example.com commit -q -m "contracts on main"

# The pull request.
sed -i.bak 's/"enum": \["USD", "EUR"\]/"enum": ["USD", "EUR", "GBP"]/' contracts/orders/v1.json && rm contracts/orders/v1.json.bak
cp "$here"/contracts/orders/v3.json contracts/orders/
cp "$here"/contracts/payments/v2.json contracts/payments/

"$bin" gate --base main
