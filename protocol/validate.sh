#!/bin/sh
# Validate protocol v1 schema and every golden fixture (JSON Schema 2020-12, ajv strict mode).
# fixtures/plaintext holds sealed-payload examples (push and answer
# plaintexts), which are not RPC exchanges and use their own wrapper schema.
# fixtures/pending holds exchanges one side does not handle yet (the helper
# or the app); they are validated here and run only by the harness of the
# side that handles them (awake.* by the helper's awake package).
# Needs Node (npx). Usage: protocol/validate.sh
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
npx -y ajv-cli@5 validate --spec=draft2020 --strict=true --strict-types=false --all-errors \
	-s "$dir/v1/fixture.schema.json" \
	-r "$dir/v1/schema.json" \
	-d "$dir/v1/fixtures/*.json"
npx -y ajv-cli@5 validate --spec=draft2020 --strict=true --strict-types=false --all-errors \
	-s "$dir/v1/plaintext.schema.json" \
	-r "$dir/v1/schema.json" \
	-d "$dir/v1/fixtures/plaintext/*.json"
set -- "$dir"/v1/fixtures/pending/*.json
if [ -e "$1" ]; then
	npx -y ajv-cli@5 validate --spec=draft2020 --strict=true --strict-types=false --all-errors \
		-s "$dir/v1/fixture.schema.json" \
		-r "$dir/v1/schema.json" \
		-d "$dir/v1/fixtures/pending/*.json"
fi
