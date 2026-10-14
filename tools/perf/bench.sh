#!/bin/sh
# compare an immutable baseline with the current source without overlapping work.
set -eu

if [ "$#" -ne 3 ]; then
    echo "usage: $0 baseline.test cpu output-directory" >&2
    exit 2
fi
baseline=$(realpath "$1")
cpu=$2
output=$3
mkdir -p "$output"
output=$(realpath "$output")
candidate="$output/current.test"
if [ "$baseline" = "$candidate" ]; then
    echo 'baseline must differ from the candidate output path' >&2
    exit 2
fi
export GOWORK=off GOMAXPROCS=1

go test -c -o "$candidate" .
{
    date -u
    go env GOVERSION GOOS GOARCH GOAMD64
    uname -sr
    lscpu
    git rev-parse HEAD
    git diff --stat
    sha256sum "$baseline" "$candidate"
    printf 'CPU=%s GOMAXPROCS=1 BENCHTIME=%s GODEBUG=%s\n' "$cpu" "${BENCHTIME:-500ms}" "${GODEBUG:-}"
} > "$output/environment.txt"
: > "$output/original.txt"
: > "$output/current.txt"

measure() {
    label=$1
    binary=$2
    taskset -c "$cpu" "$binary" -test.run '^$' \
        -test.bench '^Benchmark(Encode|Decode)$' -test.benchmem \
        -test.benchtime "${BENCHTIME:-500ms}" -test.count 1 >> "$output/$label.txt"
}

for round in 1 2 3 4 5 6; do
    if [ "$((round % 2))" -eq 1 ]; then
        measure original "$baseline"
        measure current "$candidate"
    else
        measure current "$candidate"
        measure original "$baseline"
    fi
    printf 'completed paired round %s/6\n' "$round"
done
