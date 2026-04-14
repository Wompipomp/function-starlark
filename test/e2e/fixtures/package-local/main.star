# Published OCI module that uses package-local (./) loads for its
# intra-package siblings. A consumer can load this without configuring
# ociDefaultRegistry for the sibling — ./helper.star resolves to the
# SAME artifact the caller (main.star) came from.
load("./helper.star", "helper")
load("./values.star", "greeting")

message = greeting + ", " + helper
