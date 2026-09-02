// The label and the method name are both innocent on their own, and the
// two names they derive collide: label FooQueryText gives the decode
// helper decodeFooQueryText, and method DecodeFoo gives the query-text
// const decodeFooQueryText. Both are generator-owned, so no capture
// guard sees the pair — sweepIdentifiers source 7 is what refuses it
// (bd gqlc-igs4).

// name: DecodeFoo :exec
MATCH (n:FooQueryText) DETACH DELETE n
