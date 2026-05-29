#!/usr/bin/env python3
"""Normalize `crossplane composition render` (v2) output for stable comparison.

render-check runs the crossplane v2 render engine (pinned via
--crossplane-version). Its output is deterministic except for:

  - lastTransitionTime  : wall-clock timestamps
  - uid                 : owner-reference UIDs
  - generateName        : present in some engine versions
  - document order      : composed resources and kind: Result documents are
                          emitted in map-iteration order, which varies per run

Everything else (resource bodies, spec.crossplane.resourceRefs, status
conditions, the set of Result documents) is stable. So we normalize the dynamic
fields, then sort whole document blocks by their full text to give a canonical
ordering. The expected output is generated through this same filter, so any
real difference in rendered content still surfaces in the diff.

Stdlib only — no PyYAML dependency, so it runs identically on CI and locally.
"""
import sys
import re


def normalize(doc):
    doc = re.sub(
        r'lastTransitionTime: "[^"]*"',
        'lastTransitionTime: "2024-01-01T00:00:00Z"',
        doc,
    )
    doc = re.sub(r"uid: [a-f0-9-]+", 'uid: ""', doc)
    doc = re.sub(r"^\s*generateName: .+\n", "", doc, flags=re.MULTILINE)
    return doc


raw = sys.stdin.read()
docs = [normalize(d) for d in raw.split("---\n") if d.strip()]

# Sort whole document blocks by full text for a deterministic order. The set of
# documents is identical across runs; only their emission order varies.
docs.sort()

for doc in docs:
    sys.stdout.write("---\n")
    sys.stdout.write(doc)
    if not doc.endswith("\n"):
        sys.stdout.write("\n")
