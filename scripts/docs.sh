#!/usr/bin/env bash
# Regenerates docs/*.txt: the CLI's real output for the README's examples.
# The README quotes these files verbatim, and a test checks that it still does.
set -uo pipefail
cd "$(dirname "$0")/.."
export PATH="$PWD:$PATH"
show() {
  local out=$1; shift
  { echo "\$ $*"; "$@" 2>&1; } > "docs/$out"
}
show check.txt   schema-drift check examples/contracts/orders/v2.json examples/contracts/orders/v3.json
show history.txt schema-drift history examples/contracts
show verify.txt  schema-drift verify examples/contracts/orders/v2.json examples/events/orders-mixed.ndjson
{ echo '$ examples/gate-demo.sh'; SCHEMA_DRIFT="$PWD/schema-drift" examples/gate-demo.sh 2>&1; } > docs/gate.txt
exit 0
