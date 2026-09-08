`index.json` is real output from the `Rebuild the catalogue` step of
`wyvencraft/.github/workflows/release.yml`, not something written by hand.

It is here so the publisher and the reader stay checked against each other: the
two halves of this contract live in different repositories, so nothing else
would notice the day the workflow's `jq` starts emitting a shape this package
cannot read. Regenerate it from a real run rather than editing it.
