# ISO GQL Annex D feature list — source of `codes.go`

`codes.go` vendors the bare **feature identifiers** from ISO's normative,
free-of-charge XML digital-artefact companion to ISO/IEC 39075:2024. The
descriptions in the XML are **not** vendored here; the guard that consumes
`codes.go` needs to answer *"is this a real code"*, not *"what does ISO call
it"*, so reproducing prose text carries no benefit against the additional
licence question it raises.

## Provenance

- **URL:** <https://standards.iso.org/iso-iec/39075/ed-1/en/ISO_IEC_39075(en)-features.xml>
- **Fetched:** 2026-07-26
- **SHA-256 of source XML at fetch time:**
  `ae0e8b7cbd39700092c90b93854e7a8238375e4716e96a696c247d6c0f613e56`
- **Feature count at fetch:** 228 `<feature>` elements
- **Licence:** published by ISO under the ISO Customer Licence (Freely
  Available Standards / free-of-charge digital artefacts)

## Regeneration

The list is sorted alphabetically. To re-vendor:

```bash
curl -sSL 'https://standards.iso.org/iso-iec/39075/ed-1/en/ISO_IEC_39075(en)-features.xml' \
    | grep -oE '<code>[^<]+</code>' \
    | sed 's|<code>||;s|</code>||' \
    | sort
```

## Drift check

The vendored snapshot goes stale when ISO publishes ISO/IEC 39075:2024/CD Cor
1 or a subsequent edition. `bd gqlc-4jm` is the open bead for the durable
drift check that will re-fetch the URL, recompute the SHA-256, and diff the
code list. Until that lands, this file is the manual record.

## Related note

Two independent vendor conformance tables (Neo4j, Ultipa) reproduce the same
228-code list byte-for-byte. Those tables are secondary sources and are noted
here only as corroboration of the ISO artefact.

The claim that *"mandatory GQL features are not assigned a GQL feature ID
code"* is corroborated by the same two vendor tables but has **not** been
verified against the normative ISO text (Annex A / subclause 24.3, both
paywalled). Treat that claim as secondary-sourced wherever it appears.
