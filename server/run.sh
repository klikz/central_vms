#!/bin/sh
# build papkasidan ishga tushirish (root §4.1). config: build/config/config.yaml
cd "$(dirname "$0")"
exec ./vmsd -config config/config.yaml -web web "$@"
