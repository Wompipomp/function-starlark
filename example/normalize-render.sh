#!/bin/bash
# Normalizes crossplane render output for deterministic comparison across
# crossplane CLI versions (v1 and v2).
#
# v2 additions filtered:
#   - spec.crossplane.resourceRefs block in the XR
#   - Responsive condition with real timestamps
#   - ComposeResources result documents
#   - Result field renames (reason→step, Normal→SEVERITY_NORMAL)

# 1. Remove entire YAML documents that are ComposeResources results.
awk '
BEGIN { buf = "" }
/^---$/ {
    if (buf != "" && buf !~ /reason: ComposeResources/) printf "%s", buf
    buf = "---\n"
    next
}
{ buf = buf $0 "\n" }
END { if (buf != "" && buf !~ /reason: ComposeResources/) printf "%s", buf }
' | \
# 2. Remove spec.crossplane block (resourceRefs added by v2).
#    Leaves non-crossplane spec blocks intact for composed resources.
awk '
/^spec:$/ {
    getline
    if ($0 ~ /^  crossplane:/) {
        while ((getline line) > 0) {
            if (line !~ /^  / && line !~ /^$/) { print line; break }
        }
        next
    } else {
        print "spec:"
        print
        next
    }
}
{ print }
' | \
# 3. Remove Responsive condition block (4 lines: lastTransitionTime,
#    WatchCircuitClosed, status, type).
awk '
/^  - lastTransitionTime:/ {
    buf = $0
    getline
    if ($0 ~ /reason: WatchCircuitClosed/) {
        getline; getline
        next
    }
    print buf
    print
    next
}
{ print }
' | \
# 4. Remove version-dependent noise lines.
grep -v -E '^\s*generateName:|^\s*message: .*Unready resources:' | \
# 5. Normalize timestamps to a fixed value.
sed 's/lastTransitionTime: "[^"]*"/lastTransitionTime: "2024-01-01T00:00:00Z"/g' | \
# 6. Normalize v2 Result fields to v1 format.
sed 's/^reason: /step: /' | \
sed 's/^severity: Normal$/severity: SEVERITY_NORMAL/' | \
sed 's/^severity: Warning$/severity: SEVERITY_WARNING/'
