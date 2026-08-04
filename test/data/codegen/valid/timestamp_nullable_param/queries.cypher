// One bound instant, nullable, and nothing else temporal anywhere: no
// non-nullable instant parameter and no projected instant. The nullable
// encoder is therefore the only one this batch may emit, and a gate that
// reached for the non-nullable one alongside it would leave a helper
// behind that nothing calls — invisible in every fixture that binds
// both.

// name: SamplesSeenAfter :many
MATCH (s:Sample) WHERE s.seenAt > $seenAfter RETURN s.id AS id ORDER BY s.seenAt
