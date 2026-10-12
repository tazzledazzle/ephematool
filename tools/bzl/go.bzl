"""go_test wrapper wired in via `# gazelle:map_kind` in the root BUILD.bazel.

Adds `-test.short` by default so `bazel test //...` matches the old `go test ./... -short`.
Under --config=integration (which sets --define=integration=true) the flag is dropped,
matching the old `go test -tags integration ./...`.
"""

load("@io_bazel_rules_go//go:def.bzl", _go_test = "go_test")

def go_test(name, args = [], **kwargs):
    _go_test(
        name = name,
        args = select({
            "//tools/bzl:integration": [],
            "//conditions:default": ["-test.short"],
        }) + args,
        **kwargs
    )
