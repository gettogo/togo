#!/bin/sh
# Validate protocol v1 schema and every golden fixture (JSON Schema 2020-12, ajv strict mode).
# Needs Node (npx). Usage: protocol/validate.sh
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
exec npx -y ajv-cli@5 validate --spec=draft2020 --strict=true --strict-types=false --all-errors \
	-s "$dir/v1/fixture.schema.json" \
	-r "$dir/v1/schema.json" \
	-d "$dir/v1/fixtures/*.json"
