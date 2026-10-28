// name: Blobs :many
MATCH (b:Blob) RETURN b

// A :one over the BYTES column alone. Its return type is []byte, so a failing
// read returns that type's zero, which is nil: goTypeZeroText answered the
// numeric 0 for it until bd gqlc-do1, and an emitted `return 0, err` against a
// []byte result does not compile. TestGoldenBuild holds the golden to that.
// name: BlobPayload :one
MATCH (b:Blob) WHERE b.id = $id RETURN b.payload AS payload
