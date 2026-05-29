#!/usr/bin/env python3
"""Normalize crossplane render output for v1/v2 comparison.

Handles: document ordering, extra conditions (Responsive, Synced),
condition sort order, extra results (ComposeResources, SelectComposition),
Result field renames, spec.crossplane block, dynamic timestamps/UIDs,
generateName, Unready resources message, Usage name field.
"""
import sys
import re


def extract_sort_key(doc):
    if "kind: Result" in doc:
        m = re.search(r"message: (.+)", doc)
        return ("Z", m.group(1) if m else "")
    m = re.search(r"crossplane\.io/composition-resource-name: (.+)", doc)
    if m:
        return ("B", m.group(1))
    return ("A", "")


def process_conditions(text):
    lines = text.split("\n")
    out = []
    i = 0
    while i < len(lines):
        line = lines[i]
        if re.match(r"^  conditions:\s*$", line):
            out.append(line)
            i += 1
            conditions = []
            current = []
            while i < len(lines):
                l = lines[i]
                if l.startswith("  - "):
                    if current:
                        conditions.append(current)
                    current = [l]
                    i += 1
                elif current and (l.startswith("    ") or l == ""):
                    current.append(l)
                    i += 1
                else:
                    break
            if current:
                conditions.append(current)

            skip_types = {"Responsive", "Synced"}

            def cond_type(block):
                for bl in block:
                    m = re.match(r"\s+type: (.+)", bl)
                    if m:
                        return m.group(1)
                return ""

            conditions = [c for c in conditions if cond_type(c) not in skip_types]
            conditions.sort(key=cond_type)
            for cond in conditions:
                out.extend(cond)
        else:
            out.append(line)
            i += 1
    return "\n".join(out)


def normalize(doc):
    if "reason: ComposeResources" in doc:
        return None
    if re.search(r"(step|reason): SelectComposition", doc):
        return None

    doc = re.sub(r"^spec:\n  crossplane:\n(?:    .+\n)*", "", doc, flags=re.MULTILINE)
    doc = re.sub(
        r'lastTransitionTime: "[^"]*"',
        'lastTransitionTime: "2024-01-01T00:00:00Z"',
        doc,
    )
    doc = re.sub(r"uid: [a-f0-9-]+", 'uid: ""', doc)
    doc = re.sub(r"^\s*generateName: .+\n", "", doc, flags=re.MULTILINE)
    doc = re.sub(r"^\s*message: .*Unready resources:.+\n", "", doc, flags=re.MULTILINE)
    if "kind: Usage" in doc:
        doc = re.sub(r"^  name: .+\n", "", doc, count=1, flags=re.MULTILINE)
    doc = re.sub(r"^reason: (\S+)", r"step: \1", doc, flags=re.MULTILINE)
    doc = re.sub(r"^severity: Normal$", "severity: SEVERITY_NORMAL", doc, flags=re.MULTILINE)
    doc = re.sub(r"^severity: Warning$", "severity: SEVERITY_WARNING", doc, flags=re.MULTILINE)
    doc = process_conditions(doc)
    return doc


raw = sys.stdin.read()
docs = raw.split("---\n")
normalized = []

for doc in docs:
    if not doc.strip():
        continue
    result = normalize(doc)
    if result and result.strip():
        normalized.append(result)

normalized.sort(key=extract_sort_key)

for doc in normalized:
    sys.stdout.write("---\n")
    sys.stdout.write(doc)
    if not doc.endswith("\n"):
        sys.stdout.write("\n")
